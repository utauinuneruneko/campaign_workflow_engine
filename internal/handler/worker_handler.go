package handler

import (
	"errors"
	"net/http"

	"campaign_workflow_engine/internal/clock"
	"campaign_workflow_engine/internal/worker"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type WorkerHandler struct {
	worker *worker.Worker
}

func NewWorkerHandler(db *gorm.DB) *WorkerHandler {
	return &WorkerHandler{worker: worker.NewWorker(db, clock.RealClock{})}
}

func (h *WorkerHandler) RunOnce(c *gin.Context) {
	err := h.worker.RunOnce()
	if errors.Is(err, worker.ErrNoPendingTask) {
		c.Status(http.StatusNoContent)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start workflow"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "running"})
}
