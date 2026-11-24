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

func NewWorker(db *gorm.DB, c clock.Clock) *Worker {
	if c == nil {
		c = clock.RealClock{}
	}
	return &Worker{db: db, clock: c}
}

func (w *Worker) GetDB() *gorm.DB {
	return w.db
}

func CalculateNextWorkflowBudget(
	totalDuration time.Duration,
	remainingDuration time.Duration,
	remainingBudget int64,
	workflowDuration time.Duration,
) (int64, error) {
	if err := validateBudgetInputs(
		totalDuration,
		remainingDuration,
		remainingBudget,
		workflowDuration,
	); err != nil {
		return 0, err
	}
	if workflowDuration == remainingDuration {
		return remainingBudget, nil
	}

	budget := new(big.Int).Mul(
		big.NewInt(remainingBudget),
		big.NewInt(int64(workflowDuration)),
	)
	budget.Quo(budget, big.NewInt(int64(remainingDuration)))
	if !budget.IsInt64() {
		return 0, errors.New("calculated budget overflows int64")
	}
	return budget.Int64(), nil
}

func validateBudgetInputs(
	totalDuration time.Duration,
	remainingDuration time.Duration,
	remainingBudget int64,
	workflowDuration time.Duration,
) error {
	if totalDuration <= 0 {
		return errors.New("total campaign duration must be positive")
	}
	if remainingDuration <= 0 || remainingDuration > totalDuration {
		return errors.New(
			"remaining duration must be positive and no greater than total campaign duration",
		)
	}
	if workflowDuration <= 0 || workflowDuration > remainingDuration {
		return errors.New(
			"current workflow duration must be positive and no greater than remaining duration",
		)
	}
	if remainingBudget < 0 {
		return errors.New("remaining budget cannot be negative")
	}
	return nil
}

func (w *Worker) RunOnce() error {
	found := false

	err := w.db.Transaction(func(tx *gorm.DB) error {
		now := w.clock.Now()
		if err := completeExpiredCampaigns(tx, now); err != nil {
			return err
		}

		var task model.WorkflowTask
		created, err := w.createNextDueTask(tx, now, &task)
		if err != nil {
			return fmt.Errorf("create next workflow: %w", err)
		}
		if !created {
			found, err = w.findDueTask(tx, now, &task)
			if err != nil {
				return err
			}
		} else {
			found = true
		}
		if !found {
			return nil
		}

		return w.claimTask(tx, now, task)
	})
	if err != nil {
		return err
	}
	if !found {
		return ErrNoPendingTask
	}
	return nil
}

func completeExpiredCampaigns(tx *gorm.DB, now time.Time) error {
	statuses := []model.CampaignStatus{
		model.CampaignStatusApproved,
		model.CampaignStatusDelivering,
	}

	result := tx.Model(&model.Campaign{}).
		Where("status IN ? AND end_time <= ?", statuses, now).
		Update("status", model.CampaignStatusCompleted)
	if result.Error != nil {
		return fmt.Errorf("complete expired campaigns: %w", result.Error)
	}
	return nil
}

func (w *Worker) findDueTask(
	tx *gorm.DB,
	now time.Time,
	task *model.WorkflowTask,
) (bool, error) {
	statuses := []model.CampaignStatus{
		model.CampaignStatusApproved,
		model.CampaignStatusDelivering,
	}

	err := tx.
		Joins("JOIN campaigns ON campaigns.id = workflow_tasks.campaign_id").
		Where(
			"workflow_tasks.status = ? "+
				"AND workflow_tasks.start_time <= ? "+
				"AND workflow_tasks.end_time > ? "+
				"AND campaigns.status IN ? "+
				"AND campaigns.end_time > ?",
			model.WorkflowTaskStatusPending,
			now,
			now,
			statuses,
			now,
		).
		Order("workflow_tasks.create_time ASC").
		Order("workflow_tasks.id ASC").
		First(task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find due workflow: %w", err)
	}
	return true, nil
}

func (w *Worker) claimTask(
	tx *gorm.DB,
	now time.Time,
	task model.WorkflowTask,
) error {
	result := tx.Model(&model.WorkflowTask{}).
		Where(
			"id = ? AND status = ? AND start_time <= ?",
			task.ID,
			model.WorkflowTaskStatusPending,
			now,
		).
		Update("status", model.WorkflowTaskStatusRunning)
	if result.Error != nil {
		return fmt.Errorf("mark workflow running: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil
	}

	result = tx.Model(&model.Campaign{}).
		Where("id = ? AND status = ?", task.CampaignID, model.CampaignStatusApproved).
		Update("status", model.CampaignStatusDelivering)
	if result.Error != nil {
		return fmt.Errorf("start campaign delivery: %w", result.Error)
	}
	return nil
}

func (w *Worker) createNextDueTask(
	tx *gorm.DB,
	now time.Time,
	task *model.WorkflowTask,
) (bool, error) {
	campaigns, err := w.findEligibleCampaigns(tx, now)
	if err != nil {
		return false, err
	}

	for _, campaign := range campaigns {
		next, ready, err := w.nextTaskForCampaign(tx, campaign, now)
		if err != nil {
			return false, err
		}
		if !ready {
			continue
		}

		if err := tx.Create(&next).Error; err != nil {
			return false, fmt.Errorf("create next workflow: %w", err)
		}
		*task = next
		return true, nil
	}
	return false, nil
}

func (w *Worker) findEligibleCampaigns(
	tx *gorm.DB,
	now time.Time,
) ([]model.Campaign, error) {
	statuses := []model.CampaignStatus{
		model.CampaignStatusApproved,
		model.CampaignStatusDelivering,
	}

	var campaigns []model.Campaign
	err := tx.
		Where("status IN ? AND budget > ? AND end_time > ?", statuses, 0, now).
		Order("approved_at ASC").
		Order("id ASC").
		Find(&campaigns).Error
	if err != nil {
		return nil, fmt.Errorf("find eligible campaigns: %w", err)
	}
	return campaigns, nil
}

func (w *Worker) nextTaskForCampaign(
	tx *gorm.DB,
	campaign model.Campaign,
	now time.Time,
) (model.WorkflowTask, bool, error) {
	if !campaignReadyForWorkflow(campaign, now) {
		return model.WorkflowTask{}, false, nil
	}

	previous, isFirst, err := w.findPreviousTask(tx, campaign.ID)
	if err != nil {
		return model.WorkflowTask{}, false, err
	}

	startAt, duration, ready := workflowWindow(campaign, previous, isFirst, now)
	if !ready {
		return model.WorkflowTask{}, false, nil
	}

	remainingBudget, err := w.remainingBudget(tx, campaign, isFirst)
	if err != nil {
		return model.WorkflowTask{}, false, err
	}

	remainingDuration := campaign.EndTime.Sub(startAt)
	totalDuration := campaign.EndTime.Sub(campaign.StartTime)
	budget, err := CalculateNextWorkflowBudget(
		totalDuration,
		remainingDuration,
		remainingBudget,
		duration,
	)
	if err != nil {
		return model.WorkflowTask{}, false, err
	}

	sequence, err := w.taskSequence(tx, campaign.ID)
	if err != nil {
		return model.WorkflowTask{}, false, err
	}

	return model.WorkflowTask{
		CampaignID:     campaign.ID,
		Status:         model.WorkflowTaskStatusPending,
		Budget:         budget,
		StartTime:      startAt,
		EndTime:        startAt.Add(duration),
		IdempotencyKey: fmt.Sprintf("campaign-%d-delivery-%d", campaign.ID, sequence+1),
	}, true, nil
}

func campaignReadyForWorkflow(campaign model.Campaign, now time.Time) bool {
	if campaign.ApprovedAt == nil || campaign.ApprovedAt.After(now) {
		return false
	}
	return !campaign.StartTime.After(now)
}

func (w *Worker) findPreviousTask(
	tx *gorm.DB,
	campaignID int64,
) (model.WorkflowTask, bool, error) {
	var previous model.WorkflowTask
	err := tx.Where("campaign_id = ?", campaignID).
		Order("start_time DESC").
		Order("id DESC").
		First(&previous).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.WorkflowTask{}, true, nil
	}
	if err != nil {
		return model.WorkflowTask{}, false, err
	}
	return previous, false, nil
}

func workflowWindow(
	campaign model.Campaign,
	previous model.WorkflowTask,
	isFirst bool,
	now time.Time,
) (time.Time, time.Duration, bool) {
	startAt := campaign.StartTime
	totalDuration := campaign.EndTime.Sub(campaign.StartTime)
	remainingDuration := campaign.EndTime.Sub(startAt)
	duration := WorkflowPeriod + totalDuration%WorkflowPeriod

	if !isFirst {
		if previous.Status != model.WorkflowTaskStatusSuccess &&
			previous.Status != model.WorkflowTaskStatusFailed {
			return time.Time{}, 0, false
		}
		startAt = previous.EndTime
		if startAt.After(now) || !startAt.Before(campaign.EndTime) {
			return time.Time{}, 0, false
		}
		remainingDuration = campaign.EndTime.Sub(startAt)
		duration = WorkflowPeriod
	}
	if duration > remainingDuration {
		duration = remainingDuration
	}
	return startAt, duration, duration > 0
}

func (w *Worker) remainingBudget(
	tx *gorm.DB,
	campaign model.Campaign,
	isFirst bool,
) (int64, error) {
	if isFirst {
		return campaign.Budget, nil
	}

	var allocated int64
	err := tx.Model(&model.WorkflowTask{}).
		Where("campaign_id = ?", campaign.ID).
		Select("COALESCE(SUM(budget), 0)").
		Scan(&allocated).Error
	if err != nil {
		return 0, fmt.Errorf("sum allocated workflow budgets: %w", err)
	}
	return campaign.Budget - allocated, nil
}

func (w *Worker) taskSequence(tx *gorm.DB, campaignID int64) (int64, error) {
	var sequence int64
	err := tx.Model(&model.WorkflowTask{}).
		Where("campaign_id = ?", campaignID).
		Count(&sequence).Error
	if err != nil {
		return 0, fmt.Errorf("count campaign workflows: %w", err)
	}
	return sequence, nil
}
