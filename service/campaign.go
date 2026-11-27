package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/utauinuneruneko/campaign_workflow_engine/models"
	"github.com/utauinuneruneko/campaign_workflow_engine/repo"
	"gorm.io/gorm"
)

const SubmissionExpiry = 48 * time.Hour

var (
	ErrCampaignNotFound   = errors.New("campaign not found")
	ErrInvalidTransition  = errors.New("invalid campaign status transition")
	ErrSubmissionExpired  = errors.New("campaign submission expired and was reverted to draft")
	ErrCampaignNotStarted = errors.New("campaign start time has not been reached")
)

type CampaignService struct {
	db        *gorm.DB
	now       func() time.Time
	repo      *repo.CampaignRepository
	workflows *repo.WorkflowRepository
}

func NewCampaignService(db *gorm.DB) *CampaignService {
	return &CampaignService{
		db:        db,
		now:       time.Now,
		repo:      repo.NewCampaignRepository(db),
		workflows: repo.NewWorkflowRepository(db),
	}
}

func (s *CampaignService) Submit(ctx context.Context, campaignID int64) error {
	now := s.now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		campaignRepo := s.repo.WithDB(tx)
		campaign, err := campaignRepo.GetByID(campaignID)
		if err != nil {
			return s.mapCampaignLookupError(err)
		}
		if campaign.Status != models.CampaignStatusDraft {
			return ErrInvalidTransition
		}
		_, err = campaignRepo.UpdateByID(campaignID, map[string]interface{}{
			"status":       models.CampaignStatusSubmitted,
			"submitted_at": now,
		})
		return err
	})
}

func (s *CampaignService) Approve(ctx context.Context, campaignID int64) error {
	expired := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		campaignRepo := s.repo.WithDB(tx)
		campaign, err := campaignRepo.GetByID(campaignID)
		if err != nil {
			return s.mapCampaignLookupError(err)
		}
		if campaign.Status != models.CampaignStatusSubmitted {
			return ErrInvalidTransition
		}

		now := s.now()
		if s.submissionExpired(campaign, now) {
			expired = true
			_, err := campaignRepo.UpdateByID(campaign.ID, map[string]interface{}{
				"status":       models.CampaignStatusDraft,
				"submitted_at": nil,
			})
			return err
		}
		if campaign.Budget <= 0 {
			return errors.New("campaign budget must be positive")
		}
		if _, err := campaignRepo.UpdateByID(campaign.ID, map[string]interface{}{
			"status":      models.CampaignStatusApproved,
			"approved_at": now,
		}); err != nil {
			return err
		}
		if now.Before(campaign.StartTime) {
			return nil
		}
		return s.createInitialTaskIfMissing(tx, campaign)
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
		campaign, err := s.repo.WithDB(tx).GetByID(campaignID)
		if err != nil {
			return s.mapCampaignLookupError(err)
		}
		if campaign.Status != models.CampaignStatusApproved {
			return ErrInvalidTransition
		}
		if s.now().Before(campaign.StartTime) {
			return ErrCampaignNotStarted
		}
		return s.createInitialTaskIfMissing(tx, campaign)
	})
}

func (s *CampaignService) createInitialTaskIfMissing(
	tx *gorm.DB,
	campaign models.Campaign,
) error {
	tasks, err := s.workflows.WithDB(tx).List()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.CampaignID == campaign.ID {
			return nil
		}
	}

	initialTask := models.WorkflowTask{
		CampaignID:     campaign.ID,
		Status:         models.WorkflowTaskStatusPending,
		StartTime:      campaign.StartTime,
		EndTime:        campaign.EndTime,
		Budget:         campaign.Budget,
		IdempotencyKey: fmt.Sprintf("campaign-%d-initial-workflow", campaign.ID),
	}
	return s.workflows.WithDB(tx).Create(&initialTask)
}

func (s *CampaignService) ExpireSubmissions(ctx context.Context) (int64, error) {
	campaigns, err := s.repo.List()
	if err != nil {
		return 0, err
	}

	cutoff := s.now().Add(-SubmissionExpiry)
	var expired int64
	for _, campaign := range campaigns {
		if campaign.Status != models.CampaignStatusSubmitted ||
			!s.submissionExpired(campaign, cutoff.Add(SubmissionExpiry)) {
			continue
		}
		if _, err := s.repo.UpdateByID(campaign.ID, map[string]interface{}{
			"status":       models.CampaignStatusDraft,
			"submitted_at": nil,
		}); err != nil {
			return expired, err
		}
		expired++
	}
	return expired, nil
}

func (s *CampaignService) submissionExpired(campaign models.Campaign, now time.Time) bool {
	submittedAt := campaign.SubmittedAt
	if submittedAt == nil {
		submittedAt = &campaign.ModifyTime
	}
	return now.Sub(*submittedAt) > SubmissionExpiry
}

func (s *CampaignService) transitionError(ctx context.Context, campaignID int64) error {
	_, err := s.repo.GetByID(campaignID)
	return s.mapCampaignLookupError(err)
}

func (s *CampaignService) mapCampaignLookupError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrCampaignNotFound
	}
	if err != nil {
		return fmt.Errorf("load campaign: %w", err)
	}
	return ErrInvalidTransition
}
