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
	pdfParser  *file.PDFParser
	jobChannel chan *IngestJob
	workerNum  int
	wg         sync.WaitGroup
}

type IngestJob struct {
	FileHeader *multipart.FileHeader
	FileBuffer []byte
}

func NewIngestService(ragUC *RAGUseCase, workers int) *IngestService {
	svc := &IngestService{
		ragUC:      ragUC,
		pdfParser:  file.NewPDFParse(),
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

func (s *IngestService) SubmitJob(fh *multipart.FileHeader, fb []byte) {
	s.jobChannel <- &IngestJob{
		FileHeader: fh,
		FileBuffer: fb,
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
		reader := bytes.NewReader(job.FileBuffer)
		content, err := s.pdfParser.Parse(reader, int64(len(job.FileBuffer)))
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
	fmt.Printf("📄 [Preview]: %s\n", content[:min(200, len(content))])
	err := s.ragUC.AddDocumentText(ctx, content, job.FileHeader.Filename)
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
	err = s.ragUC.BuildGraphFromText(context.Background(), graphText)
	if err != nil {
		log.Printf(" Worker %d graph extraction error: %v", workerID, err)
		// 图谱提取失败不应该标记为整个任务失败，打个日志就行
	} else {
		fmt.Printf(" Worker %d: Knowledge Graph Built!\n", workerID)
	}
	fmt.Printf("Worker %d finished file: %s\n", workerID, job.FileHeader.Filename)
}
