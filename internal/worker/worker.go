package worker

import (
	"errors"
	"fmt"
	"time"

	"campaign_workflow_engine/internal/model"
	"gorm.io/gorm"
)

var ErrNoPendingTask = errors.New("no eligible pending task")

// RunWorkerOnce completes expired deliveries and processes the oldest eligible task.
func RunWorkerOnce(db *gorm.DB) error {
	var task model.WorkflowTask
	noTask := false

	// Finalization is independent of task availability: campaigns become COMPLETED
	// only after their end time has been reached.
	err := db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Model(&model.Campaign{}).
			Where("status = ? AND end_time <= ?", model.CampaignStatusDelivering, now).
			Update("status", model.CampaignStatusCompleted).Error; err != nil {
			return fmt.Errorf("complete expired campaigns: %w", err)
		}

		err := tx.Joins("JOIN campaigns ON campaigns.id = workflow_tasks.campaign_id").
			Where("workflow_tasks.status = ? AND campaigns.status = ? AND campaigns.budget > ? AND campaigns.end_time > ?",
				model.WorkflowTaskStatusPending, model.CampaignStatusApproved, 0, now).
			Order("workflow_tasks.created_at ASC").
			Order("workflow_tasks.id ASC").
			First(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			noTask = true
			return nil
		}
		if err != nil {
			return fmt.Errorf("find eligible pending task: %w", err)
		}

		claimed := tx.Model(&model.WorkflowTask{}).
			Where("id = ? AND status = ?", task.ID, model.WorkflowTaskStatusPending).
			Update("status", model.WorkflowTaskStatusRunning)
		if claimed.Error != nil {
			return fmt.Errorf("mark task running: %w", claimed.Error)
		}
		if claimed.RowsAffected == 0 {
			noTask = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if noTask {
		return ErrNoPendingTask
	}

	processErr := db.Transaction(func(tx *gorm.DB) error {
		if task.TaskType != model.WorkflowTaskTypeStartDelivery {
			return fmt.Errorf("unsupported task type %q", task.TaskType)
		}
		result := tx.Model(&model.Campaign{}).
			Where("id = ? AND status = ? AND budget > ? AND end_time > ?",
				task.CampaignID, model.CampaignStatusApproved, 0, time.Now()).
			Update("status", model.CampaignStatusDelivering)
		if result.Error != nil {
			return fmt.Errorf("start campaign delivery: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("campaign %d is no longer eligible for delivery", task.CampaignID)
		}

		result = tx.Model(&model.WorkflowTask{}).
			Where("id = ? AND status = ?", task.ID, model.WorkflowTaskStatusRunning).
			Updates(map[string]interface{}{
				"status":        model.WorkflowTaskStatusSuccess,
				"error_message": "",
			})
		if result.Error != nil {
			return fmt.Errorf("mark task successful: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("task %d is no longer running", task.ID)
		}
		return nil
	})
	if processErr != nil {
		message := processErr.Error()
		if len(message) > 512 {
			message = message[:512]
		}
		failureErr := db.Transaction(func(tx *gorm.DB) error {
			// If processing had started, record the permitted DELIVERING -> FAILED
			// terminal transition; then persist the task failure.
			if err := tx.Model(&model.Campaign{}).
				Where("id = ? AND status = ?", task.CampaignID, model.CampaignStatusApproved).
				Update("status", model.CampaignStatusDelivering).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.Campaign{}).
				Where("id = ? AND status = ?", task.CampaignID, model.CampaignStatusDelivering).
				Update("status", model.CampaignStatusFailed).Error; err != nil {
				return err
			}
			return tx.Model(&model.WorkflowTask{}).
				Where("id = ? AND status = ?", task.ID, model.WorkflowTaskStatusRunning).
				Updates(map[string]interface{}{
					"status":        model.WorkflowTaskStatusFailed,
					"error_message": message,
				}).Error
		})
		if failureErr != nil {
			return fmt.Errorf("process task: %v; record task failure: %w", processErr, failureErr)
		}
		return processErr
	}
	return nil
}
