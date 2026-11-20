package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"campaign_workflow_engine/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type workflowDeliveryRequest struct {
	Amount float64 `json:"amount" binding:"required,gt=0"`
}

// WorkflowDelivery records a spend against one RUNNING workflow task. The task
// becomes SUCCESS only when its full allocated budget has been spent.
func (h *WorkerHandler) WorkflowDelivery(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid workflow ID"})
		return
	}
	var req workflowDeliveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var updated model.WorkflowTask
	err = h.db.Transaction(func(tx *gorm.DB) error {
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
		if campaign.Status != model.CampaignStatusDelivering || !campaign.EndTime.After(now) || !task.EndAt.After(now) {
			return errWorkflowDeliveryClosed
		}
		if req.Amount > campaign.Budget-campaign.SpentBudget {
			return errSpendExceedsCampaignBudget
		}

		spent := task.SpentBudget + req.Amount
		updates := map[string]interface{}{"spent_budget": gorm.Expr("spent_budget + ?", req.Amount)}
		if spent >= task.Budget {
			updates["status"] = model.WorkflowTaskStatusSuccess
		}
		result := tx.Model(&model.WorkflowTask{}).
			Where("id = ? AND status = ? AND spent_budget + ? <= budget", task.ID, model.WorkflowTaskStatusRunning, req.Amount).
			Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("record workflow spend: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return errSpendExceedsWorkflowBudget
		}

		result = tx.Model(&model.Campaign{}).
			Where("id = ? AND status = ? AND end_time > ? AND spent_budget + ? <= budget", campaign.ID, model.CampaignStatusDelivering, now, req.Amount).
			Update("spent_budget", gorm.Expr("spent_budget + ?", req.Amount))
		if result.Error != nil {
			return fmt.Errorf("record campaign spend: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return errSpendExceedsCampaignBudget
		}
		if err := tx.First(&updated, task.ID).Error; err != nil {
			return err
		}
		return nil
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
	errWorkflowNotRunning            = errors.New("workflow is not running")
	errWorkflowDeliveryClosed        = errors.New("workflow delivery period is closed")
	errSpendExceedsWorkflowBudget    = errors.New("amount exceeds remaining workflow budget")
	errSpendExceedsCampaignBudget   = errors.New("amount exceeds remaining campaign budget")
)
