package handler

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/models"
	"gorm.io/gorm"
	"net/http"
	"strconv"
	"time"
)

type workflowDeliveryRequest struct {
	Amount int64 `json:"amount" binding:"required,gt=0"`
}

var (
	errWorkflowNotRunning         = errors.New("workflow is not running")
	errWorkflowDeliveryClosed     = errors.New("workflow delivery period is closed")
	errSpendExceedsWorkflowBudget = errors.New("amount exceeds remaining workflow budget")
	errSpendExceedsCampaignBudget = errors.New("amount exceeds remaining campaign budget")
)

func (h *WorkerHandler) WorkflowDelivery(c *gin.Context) {
	workflowID, ok := parseWorkflowID(c)
	if !ok {
		return
	}

	var req workflowDeliveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var updated models.WorkflowTask
	err := h.worker.GetDB().Transaction(func(tx *gorm.DB) error {
		return h.recordDelivery(tx, workflowID, req.Amount, &updated)
	})
	h.respondWorkflowDelivery(c, err, updated)
}

func parseWorkflowID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid workflow ID"})
		return 0, false
	}
	return id, true
}

func (h *WorkerHandler) recordDelivery(
	tx *gorm.DB,
	workflowID int64,
	amount int64,
	updated *models.WorkflowTask,
) error {
	task, campaign, err := h.loadDeliveryEntities(tx, workflowID)
	if err != nil {
		return err
	}
	if err := validateDelivery(task, campaign, amount); err != nil {
		return err
	}
	if err := recordWorkflowSpend(tx, task, amount); err != nil {
		return err
	}
	if err := recordCampaignSpend(tx, campaign, amount); err != nil {
		return err
	}
	return tx.First(updated, task.ID).Error
}

func (h *WorkerHandler) loadDeliveryEntities(
	tx *gorm.DB,
	workflowID int64,
) (models.WorkflowTask, models.Campaign, error) {
	var task models.WorkflowTask
	if err := tx.First(&task, workflowID).Error; err != nil {
		return models.WorkflowTask{}, models.Campaign{}, err
	}

	var campaign models.Campaign
	if err := tx.First(&campaign, task.CampaignID).Error; err != nil {
		return models.WorkflowTask{}, models.Campaign{}, err
	}
	return task, campaign, nil
}

func validateDelivery(
	task models.WorkflowTask,
	campaign models.Campaign,
	amount int64,
) error {
	if task.Status != models.WorkflowTaskStatusRunning {
		return errWorkflowNotRunning
	}
	if amount > task.Budget-task.SpentBudget {
		return errSpendExceedsWorkflowBudget
	}

	now := time.Now()
	if campaign.Status != models.CampaignStatusDelivering ||
		!campaign.EndTime.After(now) ||
		!task.EndTime.After(now) {
		return errWorkflowDeliveryClosed
	}
	if amount > campaign.Budget-campaign.SpentBudget {
		return errSpendExceedsCampaignBudget
	}
	return nil
}

func recordWorkflowSpend(
	tx *gorm.DB,
	task models.WorkflowTask,
	amount int64,
) error {
	updates := map[string]interface{}{
		"spent_budget": gorm.Expr("spent_budget + ?", amount),
	}
	if task.SpentBudget+amount >= task.Budget {
		updates["status"] = models.WorkflowTaskStatusSuccess
	}

	result := tx.Model(&models.WorkflowTask{}).
		Where(
			"id = ? AND status = ? AND spent_budget + ? <= budget",
			task.ID,
			models.WorkflowTaskStatusRunning,
			amount,
		).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("record workflow spend: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return errSpendExceedsWorkflowBudget
	}
	return nil
}

func recordCampaignSpend(
	tx *gorm.DB,
	campaign models.Campaign,
	amount int64,
) error {
	now := time.Now()
	result := tx.Model(&models.Campaign{}).
		Where(
			"id = ? AND status = ? AND end_time > ? "+
				"AND spent_budget + ? <= budget",
			campaign.ID,
			models.CampaignStatusDelivering,
			now,
			amount,
		).
		Update("spent_budget", gorm.Expr("spent_budget + ?", amount))
	if result.Error != nil {
		return fmt.Errorf("record campaign spend: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return errSpendExceedsCampaignBudget
	}
	return nil
}

func (h *WorkerHandler) respondWorkflowDelivery(
	c *gin.Context,
	err error,
	updated models.WorkflowTask,
) {
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{
			"workflow": updated,
			"status":   updated.Status,
		})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
	case errors.Is(err, errWorkflowNotRunning),
		errors.Is(err, errWorkflowDeliveryClosed):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, errSpendExceedsWorkflowBudget),
		errors.Is(err, errSpendExceedsCampaignBudget):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to record workflow delivery",
		})
	}
}
