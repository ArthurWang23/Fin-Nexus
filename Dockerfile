# Stage 1: Build
FROM golang:1.24-alpine AS builder

WORKDIR /app

# 启用 toolchain 自动下载，确保使用 go.mod 中指定的版本 (go1.24.11)
ENV GOTOOLCHAIN=auto

# 预下载依赖，利用缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 编译
RUN go build -o main cmd/server/main.go

# Stage 2: Runtime
FROM alpine:latest

WORKDIR /app

# 安装 ca-certificates 否则 HTTPS 请求 (OpenAI) 会报错
RUN apk --no-cache add ca-certificates tzdata

# 从编译阶段复制二进制文件
COPY --from=builder /app/main .
# 复制配置文件
COPY --from=builder /app/configs ./configs

# 暴露端口
EXPOSE 8080

CMD ["./main"]