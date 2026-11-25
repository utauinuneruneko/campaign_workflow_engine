package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/utauinuneruneko/campaign_workflow_engine/models"
	"gorm.io/gorm"
	"time"
)

const SubmissionExpiry = 48 * time.Hour

var (
	ErrCampaignNotFound   = errors.New("campaign not found")
	ErrInvalidTransition  = errors.New("invalid campaign status transition")
	ErrSubmissionExpired  = errors.New("campaign submission expired and was reverted to draft")
	ErrCampaignNotStarted = errors.New("campaign start time has not been reached")
)

type CampaignService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewCampaignService(db *gorm.DB) *CampaignService { return &CampaignService{db: db, now: time.Now} }

func (s *CampaignService) Submit(ctx context.Context, campaignID int64) error {
	now := s.now()
	result := s.db.WithContext(ctx).Model(&models.Campaign{}).
		Where("id = ? AND status = ?", campaignID, models.CampaignStatusDraft).
		Updates(map[string]interface{}{"status": models.CampaignStatusSubmitted, "submitted_at": now})
	if result.Error != nil {
		return fmt.Errorf("submit campaign: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return s.transitionError(ctx, campaignID)
	}
	return nil
}

func (s *CampaignService) Approve(ctx context.Context, campaignID int64) error {
	expired := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var campaign models.Campaign
		if err := tx.First(&campaign, campaignID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCampaignNotFound
			}
			return err
		}
		if campaign.Status != models.CampaignStatusSubmitted {
			return ErrInvalidTransition
		}
		now := s.now()
		submittedAt := campaign.SubmittedAt
		if submittedAt == nil {
			submittedAt = &campaign.ModifyTime
		}
		if now.Sub(*submittedAt) > SubmissionExpiry {
			expired = true
			return tx.Model(&models.Campaign{}).Where("id = ? AND status = ?", campaign.ID, models.CampaignStatusSubmitted).
				Updates(map[string]interface{}{"status": models.CampaignStatusDraft, "submitted_at": nil}).Error
		}
		if campaign.Budget <= 0 {
			return errors.New("campaign budget must be positive")
		}
		result := tx.Model(&models.Campaign{}).Where("id = ? AND status = ?", campaign.ID, models.CampaignStatusSubmitted).
			Updates(map[string]interface{}{"status": models.CampaignStatusApproved, "approved_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrInvalidTransition
		}
		if !now.Before(campaign.StartTime) {
			return createInitialTask(tx, campaign)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if expired {
		return ErrSubmissionExpired
	}
	return nil
}

func (s *CampaignService) CreateInitialTaskIfDue(ctx context.Context, campaignID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var campaign models.Campaign
		if err := tx.First(&campaign, campaignID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCampaignNotFound
			}
			return err
		}
		if campaign.Status != models.CampaignStatusApproved {
			return ErrInvalidTransition
		}
		now := s.now()
		if now.Before(campaign.StartTime) {
			return ErrCampaignNotStarted
		}
		return createInitialTask(tx, campaign)
	})
}

func createInitialTask(tx *gorm.DB, campaign models.Campaign) error {
	var count int64
	if err := tx.Model(&models.WorkflowTask{}).Where("campaign_id = ?", campaign.ID).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	return tx.Create(&models.WorkflowTask{
		CampaignID:     campaign.ID,
		Status:         models.WorkflowTaskStatusPending,
		StartTime:      campaign.StartTime,
		EndTime:        campaign.EndTime,
		Budget:         campaign.Budget,
		IdempotencyKey: fmt.Sprintf("campaign-%d-initial-workflow", campaign.ID),
	}).Error
}

func (s *CampaignService) ExpireSubmissions(ctx context.Context) (int64, error) {
	cutoff := s.now().Add(-SubmissionExpiry)
	result := s.db.WithContext(ctx).Model(&models.Campaign{}).
		Where("status = ? AND COALESCE(submitted_at, modify_time) < ?", models.CampaignStatusSubmitted, cutoff).
		Updates(map[string]interface{}{"status": models.CampaignStatusDraft, "submitted_at": nil})
	return result.RowsAffected, result.Error
}

func (s *CampaignService) transitionError(ctx context.Context, campaignID int64) error {
	var campaign models.Campaign
	err := s.db.WithContext(ctx).Select("id").First(&campaign, campaignID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrCampaignNotFound
	}
	if err != nil {
		return err
	}
	return ErrInvalidTransition
}
