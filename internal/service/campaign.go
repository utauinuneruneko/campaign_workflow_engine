package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"campaign_workflow_engine/internal/model"
	"gorm.io/gorm"
)

const SubmissionExpiry = 48 * time.Hour

var (
	ErrCampaignNotFound = errors.New("campaign not found")
	ErrInvalidTransition = errors.New("invalid campaign status transition")
	ErrSubmissionExpired = errors.New("campaign submission expired and was reverted to draft")
	ErrCampaignNotStarted = errors.New("campaign start time has not been reached")
)

type CampaignService struct {
	db *gorm.DB
	now func() time.Time
}

func NewCampaignService(db *gorm.DB) *CampaignService {
	return &CampaignService{db: db, now: time.Now}
}

// Submit transitions a campaign from DRAFT to SUBMITTED and records its
// submission time for the two-day approval deadline.
func (s *CampaignService) Submit(ctx context.Context, campaignID uint) error {
	now := s.now()
	result := s.db.WithContext(ctx).Model(&model.Campaign{}).
		Where("id = ? AND status = ?", campaignID, model.CampaignStatusDraft).
		Updates(map[string]interface{}{"status": model.CampaignStatusSubmitted, "submitted_at": now})
	if result.Error != nil { return fmt.Errorf("submit campaign: %w", result.Error) }
	if result.RowsAffected == 0 { return s.transitionError(ctx, campaignID) }
	return nil
}

// Approve approves only SUBMITTED campaigns. If the campaign is already within
// its scheduled start window, it atomically creates the first pending task.
// A late approval first persists the revert to DRAFT, then returns
// ErrSubmissionExpired (the transaction must not roll that revert back).
func (s *CampaignService) Approve(ctx context.Context, campaignID uint) error {
	expired := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var campaign model.Campaign
		if err := tx.First(&campaign, campaignID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) { return ErrCampaignNotFound }
			return err
		}
		if campaign.Status != model.CampaignStatusSubmitted { return ErrInvalidTransition }
		now := s.now()
		submittedAt := campaign.SubmittedAt
		if submittedAt == nil { submittedAt = &campaign.UpdatedAt }
		if now.Sub(*submittedAt) > SubmissionExpiry {
			expired = true
			return tx.Model(&model.Campaign{}).Where("id = ? AND status = ?", campaign.ID, model.CampaignStatusSubmitted).
				Updates(map[string]interface{}{"status": model.CampaignStatusDraft, "submitted_at": nil}).Error
		}
		result := tx.Model(&model.Campaign{}).Where("id = ? AND status = ?", campaign.ID, model.CampaignStatusSubmitted).
			Update("status", model.CampaignStatusApproved)
		if result.Error != nil { return result.Error }
		if result.RowsAffected == 0 { return ErrInvalidTransition }
		if !now.Before(campaign.StartTime) {
			return createInitialTask(tx, campaign, now)
		}
		return nil
	})
	if err != nil { return err }
	if expired { return ErrSubmissionExpired }
	return nil
}

// CreateInitialTaskIfDue is intended for a scheduler/worker. It creates the
// first workflow only after approval and once StartTime has arrived.
func (s *CampaignService) CreateInitialTaskIfDue(ctx context.Context, campaignID uint) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var campaign model.Campaign
		if err := tx.First(&campaign, campaignID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) { return ErrCampaignNotFound }
			return err
		}
		if campaign.Status != model.CampaignStatusApproved { return ErrInvalidTransition }
		now := s.now()
		if now.Before(campaign.StartTime) { return ErrCampaignNotStarted }
		return createInitialTask(tx, campaign, now)
	})
}

func createInitialTask(tx *gorm.DB, campaign model.Campaign, now time.Time) error {
	var count int64
	if err := tx.Model(&model.WorkflowTask{}).Where("campaign_id = ?", campaign.ID).Count(&count).Error; err != nil { return err }
	if count != 0 { return nil }
	return tx.Create(&model.WorkflowTask{
		CampaignID: campaign.ID,
		Status: model.WorkflowTaskStatusPending,
		Type: model.WorkflowTaskTypeStartDelivery,
		StartAt: campaign.StartTime,
		EndAt: campaign.EndTime,
		Budget: campaign.Budget,
		IdempotencyKey: fmt.Sprintf("campaign-%d-initial-workflow", campaign.ID),
	}).Error
}

// ExpireSubmissions should be run periodically (for example, once per minute).
// Approval performs the same deadline check to protect against scheduler lag.
func (s *CampaignService) ExpireSubmissions(ctx context.Context) (int64, error) {
	cutoff := s.now().Add(-SubmissionExpiry)
	result := s.db.WithContext(ctx).Model(&model.Campaign{}).
		Where("status = ? AND COALESCE(submitted_at, updated_at) < ?", model.CampaignStatusSubmitted, cutoff).
		Updates(map[string]interface{}{"status": model.CampaignStatusDraft, "submitted_at": nil})
	return result.RowsAffected, result.Error
}

func (s *CampaignService) transitionError(ctx context.Context, campaignID uint) error {
	var campaign model.Campaign
	err := s.db.WithContext(ctx).Select("id").First(&campaign, campaignID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) { return ErrCampaignNotFound }
	if err != nil { return err }
	return ErrInvalidTransition
}
