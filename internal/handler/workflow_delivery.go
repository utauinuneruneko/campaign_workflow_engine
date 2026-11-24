package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/model"
	"gorm.io/gorm"
)

type workflowDeliveryRequest struct {
	Amount int64 `json:"amount" binding:"required,gt=0"`
}

func (h *WorkerHandler) WorkflowDelivery(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid workflow ID"})
		return
	}
	var req workflowDeliveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var updated model.WorkflowTask
	err = h.worker.GetDB().Transaction(func(tx *gorm.DB) error {
		var task model.WorkflowTask
		if err := tx.First(&task, id).Error; err != nil {
			return err
		}
		if task.Status != model.WorkflowTaskStatusRunning {
			return errWorkflowNotRunning
		}
		if req.Amount > task.Budget-task.SpentBudget {
			return errSpendExceedsWorkflowBudget
		}
		var campaign model.Campaign
		if err := tx.First(&campaign, task.CampaignID).Error; err != nil {
			return err
		}
		now := time.Now()
		if campaign.Status != model.CampaignStatusDelivering || !campaign.EndTime.After(now) || !task.EndTime.After(now) {
			return errWorkflowDeliveryClosed
		}
		if req.Amount > campaign.Budget-campaign.SpentBudget {
			return errSpendExceedsCampaignBudget
		}
		updates := map[string]interface{}{"spent_budget": gorm.Expr("spent_budget + ?", req.Amount)}
		if task.SpentBudget+req.Amount >= task.Budget {
			updates["status"] = model.WorkflowTaskStatusSuccess
		}
		result := tx.Model(&model.WorkflowTask{}).Where("id = ? AND status = ? AND spent_budget + ? <= budget", task.ID, model.WorkflowTaskStatusRunning, req.Amount).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("record workflow spend: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return errSpendExceedsWorkflowBudget
		}
		result = tx.Model(&model.Campaign{}).Where("id = ? AND status = ? AND end_time > ? AND spent_budget + ? <= budget", campaign.ID, model.CampaignStatusDelivering, now, req.Amount).Update("spent_budget", gorm.Expr("spent_budget + ?", req.Amount))
		if result.Error != nil {
			return fmt.Errorf("record campaign spend: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return errSpendExceedsCampaignBudget
		}
		return tx.First(&updated, task.ID).Error
	})
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"workflow": updated, "status": updated.Status})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
	case errors.Is(err, errWorkflowNotRunning), errors.Is(err, errWorkflowDeliveryClosed):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, errSpendExceedsWorkflowBudget), errors.Is(err, errSpendExceedsCampaignBudget):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record workflow delivery"})
	}
}

var (
	errWorkflowNotRunning         = errors.New("workflow is not running")
	errWorkflowDeliveryClosed     = errors.New("workflow delivery period is closed")
	errSpendExceedsWorkflowBudget = errors.New("amount exceeds remaining workflow budget")
	errSpendExceedsCampaignBudget = errors.New("amount exceeds remaining campaign budget")
)
