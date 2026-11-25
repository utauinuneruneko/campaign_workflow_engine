package service

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/utauinuneruneko/campaign_workflow_engine/internal"
	"github.com/utauinuneruneko/campaign_workflow_engine/models"
	"github.com/utauinuneruneko/campaign_workflow_engine/repo"
	"gorm.io/gorm"
)

const WorkflowPeriod = 7 * 24 * time.Hour

var ErrNoPendingTask = errors.New("no eligible pending task")

type Worker struct {
	db         *gorm.DB
	clock      internal.Clock
	campaigns  *repo.CampaignRepository
}

func NewWorker(db *gorm.DB, c internal.Clock) *Worker {
	if c == nil {
		c = internal.RealClock{}
	}
	return &Worker{
		db:        db,
		clock:     c,
		campaigns: repo.NewCampaignRepository(db),
	}
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
		return errors.New("remaining duration must be positive and no greater than total campaign duration")
	}
	if workflowDuration <= 0 || workflowDuration > remainingDuration {
		return errors.New("current workflow duration must be positive and no greater than remaining duration")
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
		if err := w.completeExpiredCampaigns(tx, now); err != nil {
			return err
		}

		var task models.WorkflowTask
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

func (w *Worker) completeExpiredCampaigns(tx *gorm.DB, now time.Time) error {
	campaigns, err := w.campaigns.WithDB(tx).List()
	if err != nil {
		return err
	}
	for _, campaign := range campaigns {
		if !campaignEndReached(campaign, now) {
			continue
		}
		if _, err := w.campaigns.WithDB(tx).UpdateByID(campaign.ID, map[string]interface{}{
			"status": models.CampaignStatusCompleted,
		}); err != nil {
			return fmt.Errorf("complete campaign %d: %w", campaign.ID, err)
		}
	}
	return nil
}

func campaignEndReached(campaign models.Campaign, now time.Time) bool {
	active := campaign.Status == models.CampaignStatusApproved ||
		campaign.Status == models.CampaignStatusDelivering
	return active && !campaign.EndTime.After(now)
}

func (w *Worker) findDueTask(
	tx *gorm.DB,
	now time.Time,
	task *models.WorkflowTask,
) (bool, error) {
	statuses := []models.CampaignStatus{
		models.CampaignStatusApproved,
		models.CampaignStatusDelivering,
	}
	var tasks []models.WorkflowTask
	if err := tx.Order("create_time ASC").Order("id ASC").Find(&tasks).Error; err != nil {
		return false, fmt.Errorf("list workflow tasks: %w", err)
	}
	campaigns, err := w.campaigns.WithDB(tx).List()
	if err != nil {
		return false, err
	}
	campaignByID := campaignMap(campaigns)
	for _, candidate := range tasks {
		campaign, exists := campaignByID[candidate.CampaignID]
		if !exists || !workflowIsDue(candidate, campaign, statuses, now) {
			continue
		}
		*task = candidate
		return true, nil
	}
	return false, nil
}

func campaignMap(campaigns []models.Campaign) map[int64]models.Campaign {
	result := make(map[int64]models.Campaign, len(campaigns))
	for _, campaign := range campaigns {
		result[campaign.ID] = campaign
	}
	return result
}

func workflowIsDue(
	task models.WorkflowTask,
	campaign models.Campaign,
	eligibleStatuses []models.CampaignStatus,
	now time.Time,
) bool {
	if task.Status != models.WorkflowTaskStatusPending ||
		task.StartTime.After(now) || !task.EndTime.After(now) ||
		!campaign.EndTime.After(now) {
		return false
	}
	for _, status := range eligibleStatuses {
		if campaign.Status == status {
			return true
		}
	}
	return false
}

func (w *Worker) claimTask(
	tx *gorm.DB,
	now time.Time,
	task models.WorkflowTask,
) error {
	result := tx.Model(&models.WorkflowTask{}).
		Where("id = ? AND status = ? AND start_time <= ?", task.ID, models.WorkflowTaskStatusPending, now).
		Update("status", models.WorkflowTaskStatusRunning)
	if result.Error != nil {
		return fmt.Errorf("mark workflow running: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil
	}

	campaign, err := w.campaigns.WithDB(tx).GetByID(task.CampaignID)
	if err != nil {
		return err
	}
	if campaign.Status == models.CampaignStatusApproved {
		if _, err := w.campaigns.WithDB(tx).UpdateByID(campaign.ID, map[string]interface{}{
			"status": models.CampaignStatusDelivering,
		}); err != nil {
			return fmt.Errorf("start campaign delivery: %w", err)
		}
	}
	return nil
}

func (w *Worker) createNextDueTask(
	tx *gorm.DB,
	now time.Time,
	task *models.WorkflowTask,
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
) ([]models.Campaign, error) {
	campaigns, err := w.campaigns.WithDB(tx).List()
	if err != nil {
		return nil, err
	}
	eligible := make([]models.Campaign, 0, len(campaigns))
	for _, campaign := range campaigns {
		if campaignEligible(campaign, now) {
			eligible = append(eligible, campaign)
		}
	}
	return eligible, nil
}

func campaignEligible(campaign models.Campaign, now time.Time) bool {
	if campaign.Budget <= 0 || !campaign.EndTime.After(now) || campaign.ApprovedAt == nil {
		return false
	}
	return campaign.Status == models.CampaignStatusApproved ||
		campaign.Status == models.CampaignStatusDelivering
}

func (w *Worker) nextTaskForCampaign(
	tx *gorm.DB,
	campaign models.Campaign,
	now time.Time,
) (models.WorkflowTask, bool, error) {
	if !campaignReadyForWorkflow(campaign, now) {
		return models.WorkflowTask{}, false, nil
	}
	previous, isFirst, err := w.findPreviousTask(tx, campaign.ID)
	if err != nil {
		return models.WorkflowTask{}, false, err
	}
	startAt, duration, ready := workflowWindow(campaign, previous, isFirst, now)
	if !ready {
		return models.WorkflowTask{}, false, nil
	}
	remainingBudget, err := w.remainingBudget(tx, campaign, isFirst)
	if err != nil {
		return models.WorkflowTask{}, false, err
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
		return models.WorkflowTask{}, false, err
	}
	sequence, err := w.taskSequence(tx, campaign.ID)
	if err != nil {
		return models.WorkflowTask{}, false, err
	}
	return models.WorkflowTask{
		CampaignID:     campaign.ID,
		Status:         models.WorkflowTaskStatusPending,
		Budget:         budget,
		StartTime:      startAt,
		EndTime:        startAt.Add(duration),
		IdempotencyKey: fmt.Sprintf("campaign-%d-delivery-%d", campaign.ID, sequence+1),
	}, true, nil
}

func campaignReadyForWorkflow(campaign models.Campaign, now time.Time) bool {
	return campaign.ApprovedAt != nil &&
		!campaign.ApprovedAt.After(now) &&
		!campaign.StartTime.After(now)
}

func (w *Worker) findPreviousTask(
	tx *gorm.DB,
	campaignID int64,
) (models.WorkflowTask, bool, error) {
	var previous models.WorkflowTask
	err := tx.Where("campaign_id = ?", campaignID).
		Order("start_time DESC").
		Order("id DESC").
		First(&previous).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.WorkflowTask{}, true, nil
	}
	if err != nil {
		return models.WorkflowTask{}, false, err
	}
	return previous, false, nil
}

func workflowWindow(
	campaign models.Campaign,
	previous models.WorkflowTask,
	isFirst bool,
	now time.Time,
) (time.Time, time.Duration, bool) {
	startAt := campaign.StartTime
	totalDuration := campaign.EndTime.Sub(campaign.StartTime)
	remainingDuration := campaign.EndTime.Sub(startAt)
	duration := WorkflowPeriod + totalDuration%WorkflowPeriod
	if !isFirst {
		if previous.Status != models.WorkflowTaskStatusSuccess &&
			previous.Status != models.WorkflowTaskStatusFailed {
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
	campaign models.Campaign,
	isFirst bool,
) (int64, error) {
	if isFirst {
		return campaign.Budget, nil
	}
	var allocated int64
	if err := tx.Model(&models.WorkflowTask{}).
		Where("campaign_id = ?", campaign.ID).
		Select("COALESCE(SUM(budget), 0)").
		Scan(&allocated).Error; err != nil {
		return 0, fmt.Errorf("sum allocated workflow budgets: %w", err)
	}
	return campaign.Budget - allocated, nil
}

func (w *Worker) taskSequence(tx *gorm.DB, campaignID int64) (int64, error) {
	var sequence int64
	if err := tx.Model(&models.WorkflowTask{}).
		Where("campaign_id = ?", campaignID).
		Count(&sequence).Error; err != nil {
		return 0, fmt.Errorf("count campaign workflows: %w", err)
	}
	return sequence, nil
}
