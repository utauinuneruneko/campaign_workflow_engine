package repo

import (
	"fmt"

	"github.com/utauinuneruneko/campaign_workflow_engine/models"
	"gorm.io/gorm"
)

// CampaignRepository is the persistence boundary for campaign records.
type CampaignRepository struct {
	db *gorm.DB
}

func NewCampaignRepository(db *gorm.DB) *CampaignRepository {
	return &CampaignRepository{db: db}
}

// WithDB returns the same repository operations bound to another DB handle,
// commonly a transaction handle.
func (r *CampaignRepository) WithDB(db *gorm.DB) *CampaignRepository {
	return NewCampaignRepository(db)
}

func (r *CampaignRepository) Create(campaign *models.Campaign) error {
	if err := r.db.Create(campaign).Error; err != nil {
		return fmt.Errorf("create campaign: %w", err)
	}
	return nil
}

func (r *CampaignRepository) List() ([]models.Campaign, error) {
	var campaigns []models.Campaign
	if err := r.db.Find(&campaigns).Error; err != nil {
		return nil, fmt.Errorf("list campaigns: %w", err)
	}
	return campaigns, nil
}

func (r *CampaignRepository) GetByID(id int64) (models.Campaign, error) {
	var campaign models.Campaign
	if err := r.db.First(&campaign, id).Error; err != nil {
		return models.Campaign{}, err
	}
	return campaign, nil
}

// UpdateByID deliberately filters only by the primary key. Callers perform
// state and business-rule checks on the loaded campaign in Go before updating.
func (r *CampaignRepository) UpdateByID(
	id int64,
	updates map[string]interface{},
) (int64, error) {
	result := r.db.Model(&models.Campaign{}).
		Where("id = ?", id).
		Updates(updates)
	if result.Error != nil {
		return 0, fmt.Errorf("update campaign %d: %w", id, result.Error)
	}
	return result.RowsAffected, nil
}

func (r *CampaignRepository) DeleteByID(id int64) error {
	if err := r.db.Delete(&models.Campaign{}, id).Error; err != nil {
		return fmt.Errorf("delete campaign %d: %w", id, err)
	}
	return nil
}
