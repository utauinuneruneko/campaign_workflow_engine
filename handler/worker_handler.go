package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal"
	"github.com/utauinuneruneko/campaign_workflow_engine/service"
	"gorm.io/gorm"
	"net/http"
)

type WorkerHandler struct {
	worker *service.Worker
}

func NewWorkerHandler(db *gorm.DB) *WorkerHandler {
	return &WorkerHandler{worker: service.NewWorker(db, internal.RealClock{})}
}

func (h *WorkerHandler) RunOnce(c *gin.Context) {
	err := h.worker.RunOnce()
	if errors.Is(err, service.ErrNoPendingTask) {
		c.Status(http.StatusNoContent)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start workflow"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "running"})
}
