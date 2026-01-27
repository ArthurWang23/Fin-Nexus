package v1

import (
	"go-nexus/internal/usecase"
	"net/http"

	"github.com/gin-gonic/gin"
)

type BriefHandler struct {
	briefUC *usecase.BriefUseCase
}

func NewBriefHandler(briefUC *usecase.BriefUseCase) *BriefHandler {
	return &BriefHandler{briefUC: briefUC}
}

func (h *BriefHandler) GetBrief(c *gin.Context) {
	date := c.Query("date") // Optional: YYYY-MM-DD
	briefs, err := h.briefUC.GetMorningBrief(c.Request.Context(), date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, briefs)
}
