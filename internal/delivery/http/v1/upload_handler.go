package v1

import (
	"go-nexus/internal/infrastructure/file"
	"go-nexus/internal/usecase"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

type UploadHandler struct {
	ingestSvc *usecase.IngestService
}

func NewUploadHandler(ingestSvc *usecase.IngestService) *UploadHandler {
	return &UploadHandler{ingestSvc: ingestSvc}
}

func (h *UploadHandler) Upload(c *gin.Context) {
	userID := c.MustGet("userID").(string)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}

	if !file.IsSupportedFileType(fileHeader.Filename) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "unsupported file type. Supported formats: pdf, txt, md, markdown",
		})
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
	h.ingestSvc.SubmitJob(fileHeader, fileBytes, userID)
	c.JSON(http.StatusOK, gin.H{
		"message":  "File uploaded and queued for processing",
		"filename": fileHeader.Filename,
	})
}
