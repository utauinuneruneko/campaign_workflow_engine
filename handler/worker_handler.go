package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal"
	"github.com/utauinuneruneko/campaign_workflow_engine/service"
	"gorm.io/gorm"
)

type WorkerHandler struct {
	worker *service.Worker
}

func NewWorkerHandler(db *gorm.DB) *WorkerHandler {
	return &WorkerHandler{worker: service.NewWorker(db, internal.RealClock{})}
}

func (h *WorkerHandler) RunOnce(c *gin.Context) {
	campaignID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || campaignID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid campaign ID"})
		return
	}

	err = h.worker.RunOnce(campaignID)
	if errors.Is(err, service.ErrNoPendingTask) {
		c.Status(http.StatusNoContent)
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to run campaign workflow"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "running", "campaign_id": campaignID})
}
