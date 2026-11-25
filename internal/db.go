package internal

import (
	"fmt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"log"
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
	//err = db.AutoMigrate(
	//	&models.Campaign{},
	//	&models.WorkflowTask{},
	//	&models.DeliveryMetric{},
	//	&models.Settlement{},
	//)
	//if err != nil {
	//	return nil, fmt.Errorf("failed to migrate database: %w", err)
	//}

	log.Println("Database ready.")
	return db, nil
}
