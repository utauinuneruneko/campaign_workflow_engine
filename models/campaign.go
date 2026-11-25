package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/utauinuneruneko/campaign_workflow_engine/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"time"
)

const MinimumCampaignDuration = 7 * 24 * time.Hour

type CampaignStatus int8

const (
	CampaignStatusDraft CampaignStatus = iota
	CampaignStatusSubmitted
	CampaignStatusApproved
	CampaignStatusDelivering
	CampaignStatusCompleted
	CampaignStatusCancelled
	CampaignStatusFailed
)

func (s CampaignStatus) IsValid() bool { return s >= CampaignStatusDraft && s <= CampaignStatusFailed }
func (s CampaignStatus) String() string {
	names := [...]string{"DRAFT", "SUBMITTED", "APPROVED", "DELIVERING", "COMPLETED", "CANCELLED", "FAILED"}
	if !s.IsValid() {
		return "UNKNOWN"
	}
	return names[s]
}
func (s CampaignStatus) Value() (driver.Value, error) {
	if !s.IsValid() {
		return nil, fmt.Errorf("invalid campaign status %d", s)
	}
	return int64(s), nil
}
func (s *CampaignStatus) Scan(value interface{}) error {
	n, err := utils.IntegerValue(value)
	if err != nil {
		return err
	}
	status := CampaignStatus(n)
	if !status.IsValid() {
		return fmt.Errorf("invalid campaign status %d", n)
	}
	*s = status
	return nil
}
func (CampaignStatus) GormDataType() string                          { return "tinyint" }
func (CampaignStatus) GormDBDataType(*gorm.DB, *schema.Field) string { return "TINYINT" }
func (s CampaignStatus) MarshalJSON() ([]byte, error)                { return json.Marshal(s.String()) }
func (s *CampaignStatus) UnmarshalJSON(data []byte) error {
	var name string
	if json.Unmarshal(data, &name) == nil {
		for status := CampaignStatusDraft; status <= CampaignStatusFailed; status++ {
			if status.String() == name {
				*s = status
				return nil
			}
		}
		return fmt.Errorf("unknown campaign status %q", name)
	}
	var n int8
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	status := CampaignStatus(n)
	if !status.IsValid() {
		return fmt.Errorf("invalid campaign status %d", n)
	}
	*s = status
	return nil
}

type Campaign struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:128;not null" json:"name"`
	StartTime    time.Time `gorm:"not null;index" json:"start_time"`
	EndTime      time.Time `gorm:"not null;index" json:"end_time"`
	AdvertiserID string    `gorm:"size:64;not null;default:'';index" json:"advertiser_id,omitempty"`
	Budget       int64     `gorm:"not null;default:0" json:"budget"`

	SubmittedAt *time.Time     `gorm:"index" json:"submitted_at,omitempty"`
	ApprovedAt  *time.Time     `json:"approved_at,omitempty"`
	Status      CampaignStatus `gorm:"type:TINYINT;not null;default:0;index" json:"status"`
	SpentBudget int64          `gorm:"not null;default:0" json:"spent_budget"`
	CreateTime  time.Time      `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	ModifyTime  time.Time      `gorm:"column:modify_time;autoUpdateTime" json:"modify_time"`
	Deleted     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (c *Campaign) BeforeCreate(tx *gorm.DB) error {
	// 创建时的严格校验
	if c.Name == "" {
		return errors.New("campaign name is required")
	}
	if c.EndTime.Sub(c.StartTime) < MinimumCampaignDuration {
		return fmt.Errorf("campaign duration must be at least %s", MinimumCampaignDuration)
	}
	if c.Budget <= 0 {
		return errors.New("initial budget must be positive")
	}
	return nil
}

func (c *Campaign) BeforeUpdate(tx *gorm.DB) error {
	// 更新时的校验：只校验被填充（非零值）的字段
	if c.Name != "" && len(c.Name) > 128 {
		return errors.New("campaign name too long")
	}

	// 如果同时尝试更新两个时间，则校验时长
	if !c.StartTime.IsZero() && !c.EndTime.IsZero() {
		if c.EndTime.Sub(c.StartTime) < MinimumCampaignDuration {
			return fmt.Errorf("campaign duration must be at least %s", MinimumCampaignDuration)
		}
	}

	return nil
}
