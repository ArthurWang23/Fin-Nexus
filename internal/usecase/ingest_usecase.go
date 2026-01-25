package usecase

import (
	"bytes"
	"context"
	"fmt"
	"go-nexus/internal/infrastructure/file"
	"log"
	"mime/multipart"
	"sync"
	"time"
)

type IngestService struct {
	ragUC      *RAGUseCase
	jobChannel chan *IngestJob
	workerNum  int
	wg         sync.WaitGroup
}

type IngestJob struct {
	FileHeader *multipart.FileHeader
	FileBuffer []byte
	UserID     string
}

func NewIngestService(ragUC *RAGUseCase, workers int) *IngestService {
	svc := &IngestService{
		ragUC:      ragUC,
		jobChannel: make(chan *IngestJob, 100),
		workerNum:  workers,
	}
	svc.StartWorkers()
	return svc
}

func (s *IngestService) StartWorkers() {
	for i := 0; i < s.workerNum; i++ {
		s.wg.Add(1)
		go func(workerID int) {
			defer s.wg.Done()
			fmt.Printf("Worker %d started\n", workerID)
			for job := range s.jobChannel {
				s.processJob(workerID, job)
			}
		}(i)
	}
}

func (s *IngestService) SubmitJob(fh *multipart.FileHeader, fb []byte, userID string) {
	s.jobChannel <- &IngestJob{
		FileHeader: fh,
		FileBuffer: fb,
		UserID:     userID,
	}
}

func (s *IngestService) processJob(workerID int, job *IngestJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	fmt.Printf("Worker %d processing file: %s\n", workerID, job.FileHeader.Filename)
	type parseResult struct {
		content string
		err     error
	}
	resultChan := make(chan parseResult, 1)
	go func() {
		// 根据文件扩展名获取合适的解析器
		parser, err := file.GetParser(job.FileHeader.Filename)
		if err != nil {
			resultChan <- parseResult{content: "", err: fmt.Errorf("unsupported file type: %w", err)}
			return
		}
		reader := bytes.NewReader(job.FileBuffer)
		content, err := parser.Parse(reader, int64(len(job.FileBuffer)))
		resultChan <- parseResult{content: content, err: err}
	}()
	var content string
	select {
	case <-ctx.Done():
		log.Printf("Worker %d TIMEOUT processing file: %s", workerID, job.FileHeader.Filename)
		return // 放弃任务
	case res := <-resultChan:
		if res.err != nil {
			log.Printf(" Worker %d parse error: %v", workerID, res.err)
			return
		}
		content = res.content
	}
	previewLen := min(200, len(content))
	if previewLen > 0 {
		fmt.Printf(" [Preview]: %s\n", content[:previewLen])
	} else {
		fmt.Printf(" [Preview]: (empty content)\n")
	}
	err := s.ragUC.AddDocumentText(ctx, content, job.FileHeader.Filename, job.UserID)
	if err != nil {
		log.Printf("Worker %d storage error: %v", workerID, err)
		return
	}
	fmt.Printf("🕸️ Worker %d: Extracting Knowledge Graph...\n", workerID)
	// 截取一部分文本防止 LLM 撑爆，或者遍历 AddDocumentText 生成的 chunks
	graphText := content
	if len(graphText) > 4000 {
		graphText = graphText[:4000]
	}
	err = s.ragUC.BuildGraphFromText(context.Background(), graphText, job.UserID)
	if err != nil {
		log.Printf(" Worker %d graph extraction error: %v", workerID, err)
		// 图谱提取失败不应该标记为整个任务失败，打个日志就行
	} else {
		fmt.Printf(" Worker %d: Knowledge Graph Built!\n", workerID)
	}
	fmt.Printf("Worker %d finished file: %s\n", workerID, job.FileHeader.Filename)
}
