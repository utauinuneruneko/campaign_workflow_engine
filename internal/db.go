package internal

import (
	"fmt"
	"log"

	"github.com/utauinuneruneko/campaign_workflow_engine/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func InitDB() (*gorm.DB, error) {
	const dsn = "campaign.db"

	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	log.Println("Running database migrations...")
	if err := database.AutoMigrate(
		&models.Campaign{},
		&models.WorkflowTask{},
		&models.DeliveryMetric{},
		&models.Settlement{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	log.Println("Database ready.")
	return database, nil
}
