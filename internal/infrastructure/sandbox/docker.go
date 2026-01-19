package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/pkg/stdcopy"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type DockerExecutor struct {
	cli *client.Client
}

func NewDockerExecutor() (*DockerExecutor, error) {
	// 初始化 Docker 客户端，自动读取环境变量 (DOCKER_HOST)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &DockerExecutor{cli: cli}, nil
}

// RunPython 执行 Python 代码并返回 Stdout/Stderr
func (e *DockerExecutor) RunPython(ctx context.Context, code string) (string, []string, error) {
	// 1. 准备配置
	// 为了安全，我们禁用网络，限制内存
	config := &container.Config{
		Image:           "go-nexus-quant:latest",
		Cmd:             []string{"python", "-u", "-c", code}, // 直接在命令行执行
		Tty:             false,
		NetworkDisabled: false, // 关键安全设置：禁止联网，防止恶意代码
	}
	hostConfig := &container.HostConfig{
		// Memory: 128 * 1024 * 1024, // 限制 128MB 内存 (可选)
		AutoRemove: false, // 我们手动删除以便获取日志
	}

	// 2. 创建容器
	resp, err := e.cli.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create container: %w", err)
	}
	containerID := resp.ID
	defer func() {
		// 强制删除容器
		removeOpts := container.RemoveOptions{Force: true}
		_ = e.cli.ContainerRemove(context.Background(), containerID, removeOpts)
	}()

	// 3. 启动容器
	if err := e.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return "", nil, fmt.Errorf("failed to start container: %w", err)
	}

	// 4. 等待执行完成 (或超时)
	statusCh, errCh := e.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return "", nil, fmt.Errorf("container execution error: %w", err)
		}
	case <-statusCh:
		// 执行完成
	case <-ctx.Done():
		return "", nil, fmt.Errorf("execution timeout")
	}
	// 5. 获取输出日志 (Stdout + Stderr)
	out, err := e.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to get logs: %w", err)
	}
	defer out.Close()

	// Docker 的日志流是多路复用的，需要用 stdcopy 解包
	var stdoutBuf, stderrBuf bytes.Buffer
	_, err = stdcopy.StdCopy(&stdoutBuf, &stderrBuf, out)
	if err != nil {
		return "", nil, fmt.Errorf("failed to demux logs: %w", err)
	}

	// 拼接结果
	result := stdoutBuf.String()
	if stderrBuf.Len() > 0 {
		result += fmt.Sprintf("\n[Error Output]:\n%s", stderrBuf.String())
	}
	if result == "" {
		result = "(No output)"
	}

	var generatedFiles []string

	// 定义宿主机存储路径 (确保这个目录存在，或者由代码创建)
	outputDir := "./public/images"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return result, nil, fmt.Errorf("failed to create local image dir: %w", err)
	}

	lines := strings.Split(stdoutBuf.String(), "\n")

	for _, line := range lines {
		cleanLine := strings.TrimSpace(line)
		if strings.HasPrefix(cleanLine, "__FILE__:") {
			filename := strings.TrimSpace(strings.TrimPrefix(cleanLine, "__FILE__:"))
			// 注意：有时候 LLM 可能会加引号，如 "__FILE__:'chart.png'"，再清理一次
			filename = strings.Trim(filename, "'\"")

			if filename != "" {
				fmt.Printf(" Detected file generation: %s, extracting...\n", filename)

				localPath, err := e.copyFileFromContainer(ctx, containerID, filename, outputDir)
				if err == nil {
					generatedFiles = append(generatedFiles, localPath)
					fmt.Printf(" Extracted to: %s\n", localPath)
				} else {
					fmt.Printf(" Extraction failed: %v\n", err)
					result += fmt.Sprintf("\n[SYSTEM ERROR]: Failed to extract file %s: %v", filename, err)
				}
			}
		}
	}

	return result, generatedFiles, nil
}

// copyFileFromContainer 从容器复制文件到宿主机
func (e *DockerExecutor) copyFileFromContainer(ctx context.Context, containerID, srcFile, destDir string) (string, error) {
	srcPath := "/app/" + srcFile

	reader, _, err := e.cli.CopyFromContainer(ctx, containerID, srcPath)
	if err != nil {
		return "", fmt.Errorf("copy from container failed: %w", err)
	}
	defer reader.Close()

	// 解压 Tar
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		if header.Typeflag == tar.TypeReg {
			// 为了防止文件名冲突，加个时间戳前缀
			localFilename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(srcFile))
			destPath := filepath.Join(destDir, localFilename)

			outFile, err := os.Create(destPath)
			if err != nil {
				return "", err
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", err
			}
			outFile.Close()
			return "/images/" + localFilename, nil
		}
	}

	return "", fmt.Errorf("file not found in container tar stream")
}
