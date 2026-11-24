package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/clock"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/worker"
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
