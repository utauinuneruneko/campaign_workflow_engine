package worker

import (
	"errors"
	"fmt"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/model"
	"gorm.io/gorm"
)

var ErrNoPendingTask = errors.New("no pending task")

// RunWorkerOnce processes the oldest pending task in a single transaction.
func RunWorkerOnce(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var task model.WorkflowTask
		if err := tx.
			Where("status = ?", "PENDING").
			Order("created_at ASC").
			Order("id ASC").
			First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNoPendingTask
			}
			return fmt.Errorf("find pending task: %w", err)
		}

		result := tx.Model(&model.WorkflowTask{}).
			Where("id = ? AND status = ?", task.ID, "PENDING").
			Update("status", "PROCESSING")
		if result.Error != nil {
			return fmt.Errorf("mark task processing: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrNoPendingTask
		}

		switch task.TaskType {
		case "REVIEW_CAMPAIGN":
			if err := updateCampaignStatus(tx, task.CampaignID, "APPROVED"); err != nil {
				return err
			}

			nextTask := model.WorkflowTask{
				CampaignID:     task.CampaignID,
				TaskType:       "START_DELIVERY",
				Status:         "PENDING",
				IdempotencyKey: fmt.Sprintf("start-delivery-after-review-%d", task.ID),
			}
			if err := tx.Create(&nextTask).Error; err != nil {
				return fmt.Errorf("create START_DELIVERY task: %w", err)
			}

		case "START_DELIVERY":
			if err := updateCampaignStatus(tx, task.CampaignID, "ACTIVE"); err != nil {
				return err
			}

		default:
			return fmt.Errorf("unsupported task type %q", task.TaskType)
		}

		if err := tx.Model(&model.WorkflowTask{}).
			Where("id = ?", task.ID).
			Update("status", "COMPLETED").Error; err != nil {
			return fmt.Errorf("mark task completed: %w", err)
		}

		return nil
	})
}

func updateCampaignStatus(tx *gorm.DB, campaignID uint, status string) error {
	result := tx.Model(&model.Campaign{}).
		Where("id = ?", campaignID).
		Update("status", status)
	if result.Error != nil {
		return fmt.Errorf("update campaign status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("campaign %d not found", campaignID)
	}
	return nil
}
