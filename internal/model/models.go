package model

import (
	"time"

	"gorm.io/gorm"
)

type CampaignStatus string

const (
	CampaignStatusDraft      CampaignStatus = "DRAFT"
	CampaignStatusSubmitted  CampaignStatus = "SUBMITTED"
	CampaignStatusApproved   CampaignStatus = "APPROVED"
	CampaignStatusDelivering CampaignStatus = "DELIVERING"
	CampaignStatusCompleted  CampaignStatus = "COMPLETED"
	CampaignStatusCancelled  CampaignStatus = "CANCELLED"
	CampaignStatusFailed     CampaignStatus = "FAILED"
)

func (s CampaignStatus) IsValid() bool {
	switch s {
	case CampaignStatusDraft, CampaignStatusSubmitted, CampaignStatusApproved,
		CampaignStatusDelivering, CampaignStatusCompleted, CampaignStatusCancelled,
		CampaignStatusFailed:
		return true
	default:
		return false
	}
}

type WorkflowTaskStatus string

const (
	WorkflowTaskStatusPending WorkflowTaskStatus = "PENDING"
	WorkflowTaskStatusRunning WorkflowTaskStatus = "RUNNING"
	WorkflowTaskStatusSuccess WorkflowTaskStatus = "SUCCESS"
	WorkflowTaskStatusFailed  WorkflowTaskStatus = "FAILED"
)

type WorkflowTaskType string

const (
	WorkflowTaskTypeStartDelivery WorkflowTaskType = "START_DELIVERY"
)

// Campaign 广告活动主表
type Campaign struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Name         string         `gorm:"size:128;not null" json:"name"`
	AdvertiserID string         `gorm:"size:64;not null;index" json:"advertiser_id"`
	Budget       float64        `gorm:"not null" json:"budget"`
	Status       CampaignStatus `gorm:"size:32;not null;default:DRAFT;index" json:"status"`
	EndTime      time.Time      `gorm:"not null;index" json:"end_time"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// WorkflowTask 工作流异步任务表
type WorkflowTask struct {
	ID             uint               `gorm:"primaryKey" json:"id"`
	CampaignID     uint               `gorm:"not null;index" json:"campaign_id"`
	TaskType       WorkflowTaskType   `gorm:"size:64;not null;index" json:"task_type"`
	Status         WorkflowTaskStatus `gorm:"size:32;not null;default:PENDING;index" json:"status"`
	IdempotencyKey string             `gorm:"size:128;not null;uniqueIndex" json:"idempotency_key"`
	RetryCount     int                `gorm:"not null;default:0" json:"retry_count"`
	MaxRetries     int                `gorm:"not null;default:3" json:"max_retries"`
	ErrorMessage   string             `gorm:"size:512" json:"error_message"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// DeliveryMetric 投放数据模拟表
type DeliveryMetric struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	CampaignID       uint      `gorm:"not null;index" json:"campaign_id"`
	Impressions      int64     `gorm:"not null" json:"impressions"`
	Clicks           int64     `gorm:"not null" json:"clicks"`
	Spend            float64   `gorm:"not null" json:"spend"`
	OrganicTraffic   int64     `gorm:"not null" json:"organic_traffic"`
	IncentiveTraffic int64     `gorm:"not null" json:"incentive_traffic"`
	ReportedAt       time.Time `gorm:"not null;index" json:"reported_at"`
}

// Settlement 结算表
type Settlement struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	CampaignID     uint      `gorm:"not null;uniqueIndex" json:"campaign_id"`
	TotalSpend     float64   `gorm:"not null" json:"total_spend"`
	ValidTraffic   int64     `gorm:"not null" json:"valid_traffic"`
	InvalidTraffic int64     `gorm:"not null" json:"invalid_traffic"`
	Status         string    `gorm:"size:32;not null" json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}
