package v1

import (
	"github.com/gin-gonic/gin"
	"go-nexus/internal/usecase"
	"io"
	"net/http"
)

type UploadHandler struct {
	ingestSvc *usecase.IngestService
}

func NewUploadHandler(ingestSvc *usecase.IngestService) *UploadHandler {
	return &UploadHandler{ingestSvc: ingestSvc}
}

func (h *UploadHandler) Upload(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	// 读入内存（简单起见）
	// 大文件应该流式读取
	f, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "error opening file",
		})
		return
	}
	defer f.Close()

	fileBytes, err := io.ReadAll(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "error reading file",
		})
		return
	}
	h.ingestSvc.SubmitJob(fileHeader, fileBytes)
	c.JSON(http.StatusOK, gin.H{
		"message":  "File uploaded and queued for processing",
		"filename": fileHeader.Filename,
	})
}
