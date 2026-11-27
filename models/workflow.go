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

type WorkflowTaskStatus int8

const (
	WorkflowTaskStatusPending WorkflowTaskStatus = iota
	WorkflowTaskStatusRunning
	WorkflowTaskStatusSuccess
	WorkflowTaskStatusFailed
)

func (s WorkflowTaskStatus) IsValid() bool {
	return s >= WorkflowTaskStatusPending && s <= WorkflowTaskStatusFailed
}
func (s WorkflowTaskStatus) String() string {
	names := [...]string{"PENDING", "RUNNING", "SUCCESS", "FAILED"}
	if !s.IsValid() {
		return "UNKNOWN"
	}
	return names[s]
}
func (s WorkflowTaskStatus) Value() (driver.Value, error) {
	if !s.IsValid() {
		return nil, fmt.Errorf("invalid workflow task status %d", s)
	}
	return int64(s), nil
}
func (s *WorkflowTaskStatus) Scan(value interface{}) error {
	n, err := utils.IntegerValue(value)
	if err != nil {
		return err
	}
	status := WorkflowTaskStatus(n)
	if !status.IsValid() {
		return fmt.Errorf("invalid workflow task status %d", n)
	}
	*s = status
	return nil
}
func (WorkflowTaskStatus) GormDataType() string                          { return "tinyint" }
func (WorkflowTaskStatus) GormDBDataType(*gorm.DB, *schema.Field) string { return "TINYINT" }
func (s WorkflowTaskStatus) MarshalJSON() ([]byte, error)                { return json.Marshal(s.String()) }
func (s *WorkflowTaskStatus) UnmarshalJSON(data []byte) error {
	var name string
	if json.Unmarshal(data, &name) == nil {
		for status := WorkflowTaskStatusPending; status <= WorkflowTaskStatusFailed; status++ {
			if status.String() == name {
				*s = status
				return nil
			}
		}
		return fmt.Errorf("unknown workflow task status %q", name)
	}
	var n int8
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	status := WorkflowTaskStatus(n)
	if !status.IsValid() {
		return fmt.Errorf("invalid workflow task status %d", n)
	}
	*s = status
	return nil
}

type WorkflowTask struct {
	ID             int64              `gorm:"primaryKey" json:"id"`
	CampaignID     int64              `gorm:"not null;index" json:"campaign_id"`
	Status         WorkflowTaskStatus `gorm:"type:TINYINT;not null;default:0;index" json:"status"`
	Budget         int64              `gorm:"not null;default:0" json:"budget"`
	SpentBudget    int64              `gorm:"not null;default:0" json:"spent_budget"`
	StartTime      time.Time          `gorm:"index" json:"start_time"`
	// EndTime is the scheduled end of this workflow window.
	EndTime time.Time `gorm:"index" json:"end_time"`
	// ActualEndTime records when the task actually reached a terminal state.
	// It remains nil while the task is pending or running.
	ActualEndTime  *time.Time `gorm:"index" json:"actual_end_time,omitempty"`
	IdempotencyKey string     `gorm:"size:128;not null;uniqueIndex" json:"idempotency_key"`
	RetryCount     int        `gorm:"not null;default:0" json:"retry_count"`
	MaxRetries     int        `gorm:"not null;default:3" json:"max_retries"`
	ErrorMessage   string     `gorm:"size:512" json:"error_message"`
	CreateTime     time.Time  `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	ModifyTime     time.Time  `gorm:"column:modify_time;autoUpdateTime" json:"modify_time"`
}

func (t *WorkflowTask) BeforeSave(*gorm.DB) error {
	if !t.Status.IsValid() {
		return fmt.Errorf("invalid workflow task status %d", t.Status)
	}
	if t.Budget < 0 || t.SpentBudget < 0 || t.SpentBudget > t.Budget {
		return errors.New("workflow budget values are invalid")
	}
	return nil
}
