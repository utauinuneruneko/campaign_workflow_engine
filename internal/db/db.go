package db

import (
	"fmt"
	"log"

	"github.com/utauinuneruneko/campaign_workflow_engine/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func InitDB() (*gorm.DB, error) {
	// 数据库文件名
	dsn := "campaign.db"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	log.Println("Running database migrations...")

	// 自动建表
	err = db.AutoMigrate(
		&model.Campaign{},
		&model.WorkflowTask{},
		&model.DeliveryMetric{},
		&model.Settlement{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	log.Println("Database ready.")
	return db, nil
}
