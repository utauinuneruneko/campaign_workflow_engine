package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
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
	if !s.IsValid() { return "UNKNOWN" }
	return names[s]
}
func (s CampaignStatus) Value() (driver.Value, error) {
	if !s.IsValid() { return nil, fmt.Errorf("invalid campaign status %d", s) }
	return int64(s), nil
}
func (s *CampaignStatus) Scan(value interface{}) error {
	n, err := integerValue(value); if err != nil { return err }
	status := CampaignStatus(n); if !status.IsValid() { return fmt.Errorf("invalid campaign status %d", n) }
	*s = status; return nil
}
func (CampaignStatus) GormDataType() string { return "tinyint" }
func (CampaignStatus) GormDBDataType(*gorm.DB, *schema.Field) string { return "TINYINT" }
func (s CampaignStatus) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }
func (s *CampaignStatus) UnmarshalJSON(data []byte) error {
	var name string
	if json.Unmarshal(data, &name) == nil {
		for status := CampaignStatusDraft; status <= CampaignStatusFailed; status++ { if status.String() == name { *s = status; return nil } }
		return fmt.Errorf("unknown campaign status %q", name)
	}
	var n int8; if err := json.Unmarshal(data, &n); err != nil { return err }
	status := CampaignStatus(n); if !status.IsValid() { return fmt.Errorf("invalid campaign status %d", n) }; *s = status; return nil
}

type WorkflowTaskStatus int8
const (
	WorkflowTaskStatusPending WorkflowTaskStatus = iota
	WorkflowTaskStatusRunning
	WorkflowTaskStatusSuccess
	WorkflowTaskStatusFailed
)
func (s WorkflowTaskStatus) IsValid() bool { return s >= WorkflowTaskStatusPending && s <= WorkflowTaskStatusFailed }
func (s WorkflowTaskStatus) String() string {
	names := [...]string{"PENDING", "RUNNING", "SUCCESS", "FAILED"}; if !s.IsValid() { return "UNKNOWN" }; return names[s]
}
func (s WorkflowTaskStatus) Value() (driver.Value, error) {
	if !s.IsValid() { return nil, fmt.Errorf("invalid workflow task status %d", s) }; return int64(s), nil
}
func (s *WorkflowTaskStatus) Scan(value interface{}) error {
	n, err := integerValue(value); if err != nil { return err }; status := WorkflowTaskStatus(n)
	if !status.IsValid() { return fmt.Errorf("invalid workflow task status %d", n) }; *s = status; return nil
}
func (WorkflowTaskStatus) GormDataType() string { return "tinyint" }
func (WorkflowTaskStatus) GormDBDataType(*gorm.DB, *schema.Field) string { return "TINYINT" }
func (s WorkflowTaskStatus) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }
func (s *WorkflowTaskStatus) UnmarshalJSON(data []byte) error {
	var name string
	if json.Unmarshal(data, &name) == nil {
		for status := WorkflowTaskStatusPending; status <= WorkflowTaskStatusFailed; status++ { if status.String() == name { *s = status; return nil } }
		return fmt.Errorf("unknown workflow task status %q", name)
	}
	var n int8; if err := json.Unmarshal(data, &n); err != nil { return err }
	status := WorkflowTaskStatus(n); if !status.IsValid() { return fmt.Errorf("invalid workflow task status %d", n) }; *s = status; return nil
}

func integerValue(value interface{}) (int64, error) {
	switch v := value.(type) {
	case int64: return v, nil
	case int: return int64(v), nil
	case int32: return int64(v), nil
	case int8: return int64(v), nil
	case []byte: return strconv.ParseInt(string(v), 10, 64)
	case string: return strconv.ParseInt(v, 10, 64)
	case nil: return 0, errors.New("cannot scan NULL enum")
	default: return 0, fmt.Errorf("cannot scan enum from %T", value)
	}
}

type WorkflowTaskType string
const WorkflowTaskTypeStartDelivery WorkflowTaskType = "START_DELIVERY"

type Campaign struct {
	ID           int64          `gorm:"primaryKey" json:"id"`
	Name         string         `gorm:"size:128;not null" json:"name"`
	StartTime    time.Time      `gorm:"not null;index" json:"start_time"`
	EndTime      time.Time      `gorm:"not null;index" json:"end_time"`
	AdvertiserID string         `gorm:"size:64;not null;default:'';index" json:"advertiser_id,omitempty"`
	Budget       int64          `gorm:"not null;default:0" json:"budget"`
	SubmittedAt  *time.Time     `gorm:"index" json:"submitted_at,omitempty"`
	ApprovedAt   *time.Time     `json:"approved_at,omitempty"`
	Status       CampaignStatus `gorm:"type:TINYINT;not null;default:0;index" json:"status"`
	SpentBudget  int64          `gorm:"not null;default:0" json:"spent_budget"`
	CreateTime   time.Time      `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	ModifyTime   time.Time      `gorm:"column:modify_time;autoUpdateTime" json:"modify_time"`
	Deleted      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (c *Campaign) BeforeSave(*gorm.DB) error {
	if c.Name == "" { return errors.New("campaign name is required") }
	if c.EndTime.Sub(c.StartTime) < MinimumCampaignDuration { return fmt.Errorf("campaign duration must be at least %s", MinimumCampaignDuration) }
	if !c.Status.IsValid() { return fmt.Errorf("invalid campaign status %d", c.Status) }
	if c.Budget < 0 || c.SpentBudget < 0 || c.SpentBudget > c.Budget { return errors.New("campaign budget values are invalid") }
	return nil
}

type WorkflowTask struct {
	ID             int64               `gorm:"primaryKey" json:"id"`
	CampaignID     int64               `gorm:"not null;index" json:"campaign_id"`
	Status         WorkflowTaskStatus  `gorm:"type:TINYINT;not null;default:0;index" json:"status"`
	Type           WorkflowTaskType    `gorm:"size:64;not null;index" json:"type"`
	Budget         int64               `gorm:"not null;default:0" json:"budget"`
	SpentBudget    int64               `gorm:"not null;default:0" json:"spent_budget"`
	StartAt        time.Time           `gorm:"index" json:"start_time"`
	EndAt          time.Time           `gorm:"index" json:"end_time"`
	IdempotencyKey string              `gorm:"size:128;not null;uniqueIndex" json:"idempotency_key"`
	RetryCount     int                 `gorm:"not null;default:0" json:"retry_count"`
	MaxRetries     int                 `gorm:"not null;default:3" json:"max_retries"`
	ErrorMessage   string              `gorm:"size:512" json:"error_message"`
	CreateTime     time.Time           `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	ModifyTime     time.Time           `gorm:"column:modify_time;autoUpdateTime" json:"modify_time"`
}
func (t *WorkflowTask) BeforeSave(*gorm.DB) error {
	if !t.Status.IsValid() { return fmt.Errorf("invalid workflow task status %d", t.Status) }
	if t.Budget < 0 || t.SpentBudget < 0 || t.SpentBudget > t.Budget { return errors.New("workflow budget values are invalid") }
	return nil
}

type DeliveryMetric struct {
	ID int64 `gorm:"primaryKey" json:"id"`
	CampaignID int64 `gorm:"not null;index" json:"campaign_id"`
	Impressions int64 `gorm:"not null" json:"impressions"`
	Clicks int64 `gorm:"not null" json:"clicks"`
	Spend float64 `gorm:"not null" json:"spend"`
	OrganicTraffic int64 `gorm:"not null" json:"organic_traffic"`
	IncentiveTraffic int64 `gorm:"not null" json:"incentive_traffic"`
	ReportedAt time.Time `gorm:"not null;index" json:"reported_at"`
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	ModifyTime time.Time `gorm:"column:modify_time;autoUpdateTime" json:"modify_time"`
}

type Settlement struct {
	ID int64 `gorm:"primaryKey" json:"id"`
	CampaignID int64 `gorm:"not null;uniqueIndex" json:"campaign_id"`
	TotalSpend int64 `gorm:"not null" json:"total_spend"`
	ValidTraffic int64 `gorm:"not null" json:"valid_traffic"`
	InvalidTraffic int64 `gorm:"not null" json:"invalid_traffic"`
	Status string `gorm:"size:32;not null" json:"status"`
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	ModifyTime time.Time `gorm:"column:modify_time;autoUpdateTime" json:"modify_time"`
}
