package worker

import (
	"errors"
	"fmt"
	"time"

	"campaign_workflow_engine/internal/model"
	"gorm.io/gorm"
)

var ErrNoPendingTask = errors.New("no eligible pending task")

// RunWorkerOnce completes expired campaign deliveries and starts the oldest
// scheduled workflow. Delivery spending is recorded separately by the API.
func RunWorkerOnce(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Model(&model.Campaign{}).
			Where("status = ? AND end_time <= ?", model.CampaignStatusDelivering, now).
			Update("status", model.CampaignStatusCompleted).Error; err != nil {
			return fmt.Errorf("complete expired campaigns: %w", err)
		}

		var task model.WorkflowTask
		err := tx.Joins("JOIN campaigns ON campaigns.id = workflow_tasks.campaign_id").
			Where("workflow_tasks.status = ? AND campaigns.status IN ? AND campaigns.budget > ? AND campaigns.end_time > ? AND workflow_tasks.start_at <= ? AND workflow_tasks.end_at > ?",
				model.WorkflowTaskStatusPending,
				[]model.CampaignStatus{model.CampaignStatusApproved, model.CampaignStatusDelivering},
				0, now, now, now).
			Order("workflow_tasks.created_at ASC").
			Order("workflow_tasks.id ASC").
			First(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNoPendingTask
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
			return ErrNoPendingTask
		}

		// The campaign enters delivery when its first workflow starts. Additional
		// workflow tasks may run while the campaign is already DELIVERING.
		started := tx.Model(&model.Campaign{}).
			Where("id = ? AND status = ? AND budget > ? AND end_time > ?",
				task.CampaignID, model.CampaignStatusApproved, 0, now).
			Update("status", model.CampaignStatusDelivering)
		if started.Error != nil {
			return fmt.Errorf("start campaign delivery: %w", started.Error)
		}
		if started.RowsAffected == 0 {
			var campaign model.Campaign
			if err := tx.First(&campaign, task.CampaignID).Error; err != nil {
				return fmt.Errorf("load campaign: %w", err)
			}
			if campaign.Status != model.CampaignStatusDelivering || campaign.Budget <= 0 || !campaign.EndTime.After(now) {
				return fmt.Errorf("campaign %d is not eligible for delivery", task.CampaignID)
			}
		}
		return nil
	})
}
