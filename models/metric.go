package models

import (
	"time"
)

type DeliveryMetric struct {
	ID               int64     `gorm:"primaryKey" json:"id"`
	CampaignID       int64     `gorm:"not null;index" json:"campaign_id"`
	Impressions      int64     `gorm:"not null" json:"impressions"`
	Clicks           int64     `gorm:"not null" json:"clicks"`
	Spend            float64   `gorm:"not null" json:"spend"`
	OrganicTraffic   int64     `gorm:"not null" json:"organic_traffic"`
	IncentiveTraffic int64     `gorm:"not null" json:"incentive_traffic"`
	ReportedAt       time.Time `gorm:"not null;index" json:"reported_at"`
	CreateTime       time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	ModifyTime       time.Time `gorm:"column:modify_time;autoUpdateTime" json:"modify_time"`
}

type Settlement struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	CampaignID     int64     `gorm:"not null;uniqueIndex" json:"campaign_id"`
	TotalSpend     int64     `gorm:"not null" json:"total_spend"`
	ValidTraffic   int64     `gorm:"not null" json:"valid_traffic"`
	InvalidTraffic int64     `gorm:"not null" json:"invalid_traffic"`
	Status         string    `gorm:"size:32;not null" json:"status"`
	CreateTime     time.Time `gorm:"column:create_time;autoCreateTime" json:"create_time"`
	ModifyTime     time.Time `gorm:"column:modify_time;autoUpdateTime" json:"modify_time"`
}
