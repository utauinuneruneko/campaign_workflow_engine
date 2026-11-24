package worker

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/utauinuneruneko/campaign_workflow_engine/internal/clock"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/model"
	"gorm.io/gorm"
)

const WorkflowPeriod = 7 * 24 * time.Hour

var ErrNoPendingTask = errors.New("no eligible pending task")

type Worker struct {
	db    *gorm.DB
	clock clock.Clock
}

func NewWorker(db *gorm.DB, clock clock.Clock) *Worker {
	if clock == nil {
		clock = clockpkgReal{}
	}
	return &Worker{db: db, clock: clock}
}

func (w *Worker) GetDB() *gorm.DB {
	return w.db
}

// CalculateNextWorkflowBudget allocates integer currency units proportionally.
// The final workflow gets the remaining budget to avoid rounding residue.
func CalculateNextWorkflowBudget(totalCampaignDuration, remainingDuration time.Duration, remainingBudget int64, currentWorkflowDuration time.Duration) (int64, error) {
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
	n := new(big.Int).Mul(big.NewInt(remainingBudget), big.NewInt(int64(currentWorkflowDuration)))
	n.Quo(n, big.NewInt(int64(remainingDuration)))
	if !n.IsInt64() {
		return 0, errors.New("calculated budget overflows int64")
	}
	return n.Int64(), nil
}

// RunOnce completes expired campaigns, creates a due workflow if needed, and
// transitions one due PENDING workflow to RUNNING. A later workflow is only
// created after the previous workflow has reached SUCCESS or FAILED.
func (w *Worker) RunOnce() error {
	found := false
	err := w.db.Transaction(func(tx *gorm.DB) error {
		now := w.clock.Now()
		if err := tx.Model(&model.Campaign{}).
			Where("status IN ? AND end_time <= ?", []model.CampaignStatus{model.CampaignStatusApproved, model.CampaignStatusDelivering}, now).
			Update("status", model.CampaignStatusCompleted).Error; err != nil {
			return fmt.Errorf("complete expired campaigns: %w", err)
		}

		var task model.WorkflowTask
		created, err := w.createNextDueTask(tx, now, &task)
		if err != nil {
			return fmt.Errorf("create next workflow: %w", err)
		}
		if created {
			found = true
		} else {
			err := tx.Joins("JOIN campaigns ON campaigns.id = workflow_tasks.campaign_id").
				Where("workflow_tasks.status = ? AND workflow_tasks.start_time <= ? AND workflow_tasks.end_time > ? AND campaigns.status IN ? AND campaigns.end_time > ?",
					model.WorkflowTaskStatusPending, now, now,
					[]model.CampaignStatus{model.CampaignStatusApproved, model.CampaignStatusDelivering}, now).
				Order("workflow_tasks.create_time ASC").Order("workflow_tasks.id ASC").First(&task).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("find due workflow: %w", err)
			}
			found = true
		}

		claim := tx.Model(&model.WorkflowTask{}).
			Where("id = ? AND status = ? AND start_time <= ?", task.ID, model.WorkflowTaskStatusPending, now).
			Update("status", model.WorkflowTaskStatusRunning)
		if claim.Error != nil {
			return fmt.Errorf("mark workflow running: %w", claim.Error)
		}
		if claim.RowsAffected == 0 {
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

func (w *Worker) createNextDueTask(tx *gorm.DB, now time.Time, task *model.WorkflowTask) (bool, error) {
	var campaigns []model.Campaign
	if err := tx.Where("status IN ? AND budget > ? AND end_time > ?",
		[]model.CampaignStatus{model.CampaignStatusApproved, model.CampaignStatusDelivering}, 0, now).
		Order("approved_at ASC").Order("id ASC").Find(&campaigns).Error; err != nil {
		return false, err
	}

	for _, campaign := range campaigns {
		if campaign.ApprovedAt == nil || campaign.ApprovedAt.After(now) || campaign.StartTime.After(now) {
			continue
		}

		var previous model.WorkflowTask
		previousErr := tx.Where("campaign_id = ?", campaign.ID).
			Order("start_time DESC").Order("id DESC").First(&previous).Error
		isFirst := errors.Is(previousErr, gorm.ErrRecordNotFound)
		if previousErr != nil && !isFirst {
			return false, previousErr
		}

		startAt := campaign.StartTime
		totalDuration := campaign.EndTime.Sub(campaign.StartTime)
		remainingDuration := campaign.EndTime.Sub(startAt)
		duration := WorkflowPeriod + totalDuration%WorkflowPeriod
		if !isFirst {
			if previous.Status != model.WorkflowTaskStatusSuccess && previous.Status != model.WorkflowTaskStatusFailed {
				continue
			}
			startAt = previous.EndTime
			if startAt.After(now) || !startAt.Before(campaign.EndTime) {
				continue
			}
			remainingDuration = campaign.EndTime.Sub(startAt)
			duration = WorkflowPeriod
		}
		if duration > remainingDuration {
			duration = remainingDuration
		}
		if duration <= 0 {
			continue
		}

		remainingBudget := campaign.Budget
		if !isFirst {
			var allocated int64
			if err := tx.Model(&model.WorkflowTask{}).Where("campaign_id = ?", campaign.ID).
				Select("COALESCE(SUM(budget), 0)").Scan(&allocated).Error; err != nil {
				return false, fmt.Errorf("sum allocated workflow budgets: %w", err)
			}
			remainingBudget -= allocated
		}
		budget, err := CalculateNextWorkflowBudget(totalDuration, remainingDuration, remainingBudget, duration)
		if err != nil {
			return false, err
		}

		var sequence int64
		if err := tx.Model(&model.WorkflowTask{}).Where("campaign_id = ?", campaign.ID).Count(&sequence).Error; err != nil {
			return false, err
		}
		next := model.WorkflowTask{
			CampaignID:     campaign.ID,
			Status:         model.WorkflowTaskStatusPending,
			Budget:         budget,
			StartTime:      startAt,
			EndTime:        startAt.Add(duration),
			IdempotencyKey: fmt.Sprintf("campaign-%d-delivery-%d", campaign.ID, sequence+1),
		}
		if err := tx.Create(&next).Error; err != nil {
			return false, err
		}
		*task = next
		return true, nil
	}
	return false, nil
}

// clockpkgReal is an alias adapter, allowing NewWorker's clock parameter to
// remain named clock while still providing a safe default when it is nil.
type clockpkgReal struct{}

func (clockpkgReal) Now() time.Time { return time.Now() }
