package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/worker"
	"gorm.io/gorm"
	"net/http"
)

type WorkerHandler struct {
	db *gorm.DB
}

func NewWorkerHandler(db *gorm.DB) *WorkerHandler {
	return &WorkerHandler{db: db}
}

func (h *WorkerHandler) RunOnce(c *gin.Context) {
	err := worker.RunWorkerOnce(h.db)
	if errors.Is(err, worker.ErrNoPendingTask) {
		c.Status(http.StatusNoContent)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process task"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "completed"})
}
