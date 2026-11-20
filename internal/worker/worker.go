package worker

import (
	"errors"
	"fmt"
	"time"

	"campaign_workflow_engine/internal/model"
	"gorm.io/gorm"
)

const WorkflowPeriod = 7 * 24 * time.Hour

var ErrNoPendingTask = errors.New("no eligible pending task")

// CalculateNextWorkflowBudget allocates the remaining budget proportionally to
// the duration of the next workflow. If it is the final workflow, it receives
// all remaining budget to avoid floating-point rounding residue.
func CalculateNextWorkflowBudget(
	totalCampaignDuration time.Duration,
	remainingDuration time.Duration,
	remainingBudget float64,
	currentWorkflowDuration time.Duration,
) (float64, error) {
	if totalCampaignDuration <= 0 {
		return 0, errors.New("total campaign duration must be positive")
	}
	if remainingDuration <= 0 || remainingDuration > totalCampaignDuration {
		return 0, errors.New("remaining duration must be positive and no greater than total campaign duration")
	}
	if currentWorkflowDuration <= 0 || currentWorkflowDuration > remainingDuration {
		return 0, errors.New("current workflow duration must be positive and no greater than remaining duration")
	}
	if remainingBudget < 0 {
		return 0, errors.New("remaining budget cannot be negative")
	}
	if currentWorkflowDuration == remainingDuration {
		return remainingBudget, nil
	}
	return remainingBudget * float64(currentWorkflowDuration) / float64(remainingDuration), nil
}

// RunWorkerOnce finishes campaigns whose end time has arrived, creates the
// next workflow only when its scheduled start is due and the prior workflow is
// terminal, then moves that workflow from PENDING to RUNNING.
func RunWorkerOnce(db *gorm.DB) error {
	var task model.WorkflowTask
	found := false
	err := db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Model(&model.Campaign{}).
			Where("status = ? AND end_time <= ?", model.CampaignStatusDelivering, now).
			Update("status", model.CampaignStatusCompleted).Error; err != nil {
			return fmt.Errorf("complete expired campaigns: %w", err)
		}

		campaigns := make([]model.Campaign, 0)
		if err := tx.Where("status IN ? AND budget > ? AND end_time > ?",
			[]model.CampaignStatus{model.CampaignStatusApproved, model.CampaignStatusDelivering}, 0, now).
			Order("started_at ASC").Order("id ASC").Find(&campaigns).Error; err != nil {
			return fmt.Errorf("find eligible campaigns: %w", err)
		}

		for _, campaign := range campaigns {
			if campaign.StartedAt == nil || campaign.StartedAt.After(now) {
				continue
			}

			var previous model.WorkflowTask
			err := tx.Where("campaign_id = ?", campaign.ID).
				Order("start_at DESC").Order("id DESC").First(&previous).Error
			var startAt time.Time
			var duration time.Duration
			totalDuration := campaign.EndTime.Sub(*campaign.StartedAt)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				startAt = *campaign.StartedAt
				if startAt.After(now) {
					continue
				}
				// Use floor(total/7) workflows, making the first period 7 days
				// plus any remainder. Every period is therefore at least 7 days.
				duration = WorkflowPeriod + totalDuration%WorkflowPeriod
			} else if err != nil {
				return fmt.Errorf("find previous workflow: %w", err)
			} else {
				if previous.Status != model.WorkflowTaskStatusSuccess && previous.Status != model.WorkflowTaskStatusFailed {
					// Enforce a single active or pending workflow per campaign.
					continue
				}
				startAt = previous.EndAt
				if startAt.After(now) || !startAt.Before(campaign.EndTime) {
					continue
				}
				duration = WorkflowPeriod
			}

			remainingDuration := campaign.EndTime.Sub(startAt)
			if duration > remainingDuration {
				duration = remainingDuration
			}
			if duration <= 0 {
				continue
			}

			var allocated float64
			if errors.Is(err, gorm.ErrRecordNotFound) {
				allocated, err = CalculateNextWorkflowBudget(totalDuration, totalDuration, campaign.Budget, duration)
			} else {
				var allocatedBudget float64
				if err := tx.Model(&model.WorkflowTask{}).Where("campaign_id = ?", campaign.ID).
					Select("COALESCE(SUM(budget), 0)").Scan(&allocatedBudget).Error; err != nil {
					return fmt.Errorf("sum allocated workflow budgets: %w", err)
				}
				remainingBudget := campaign.Budget - allocatedBudget
				allocated, err = CalculateNextWorkflowBudget(totalDuration, remainingDuration, remainingBudget, duration)
			}
			if err != nil {
				return fmt.Errorf("calculate workflow budget: %w", err)
			}

			var sequence int64
			if err := tx.Model(&model.WorkflowTask{}).Where("campaign_id = ?", campaign.ID).Count(&sequence).Error; err != nil {
				return fmt.Errorf("count campaign workflows: %w", err)
			}
			next := model.WorkflowTask{
				CampaignID:     campaign.ID,
				TaskType:       model.WorkflowTaskTypeStartDelivery,
				Status:         model.WorkflowTaskStatusPending,
				Budget:         allocated,
				StartAt:        startAt,
				EndAt:          startAt.Add(duration),
				IdempotencyKey: fmt.Sprintf("campaign-%d-delivery-%d", campaign.ID, sequence+1),
			}
			if err := tx.Create(&next).Error; err != nil {
				return fmt.Errorf("create next workflow: %w", err)
			}
			task = next
			found = true
			break
		}

		if !found {
			// A previously created task may be due now, but no new task was
			// necessary. Look for the oldest eligible pending task.
			err := tx.Joins("JOIN campaigns ON campaigns.id = workflow_tasks.campaign_id").
				Where("workflow_tasks.status = ? AND workflow_tasks.start_at <= ? AND workflow_tasks.end_at > ? AND campaigns.status IN ? AND campaigns.end_time > ?",
					model.WorkflowTaskStatusPending, now, now,
					[]model.CampaignStatus{model.CampaignStatusApproved, model.CampaignStatusDelivering}, now).
				Order("workflow_tasks.created_at ASC").Order("workflow_tasks.id ASC").First(&task).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("find due workflow: %w", err)
			}
			found = true
		}

		claimed := tx.Model(&model.WorkflowTask{}).
			Where("id = ? AND status = ? AND start_at <= ?", task.ID, model.WorkflowTaskStatusPending, now).
			Update("status", model.WorkflowTaskStatusRunning)
		if claimed.Error != nil {
			return fmt.Errorf("mark workflow running: %w", claimed.Error)
		}
		if claimed.RowsAffected == 0 {
			found = false
			return nil
		}
		if err := tx.Model(&model.Campaign{}).
			Where("id = ? AND status = ?", task.CampaignID, model.CampaignStatusApproved).
			Update("status", model.CampaignStatusDelivering).Error; err != nil {
			return fmt.Errorf("start campaign delivery: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return ErrNoPendingTask
	}
	return nil
}
