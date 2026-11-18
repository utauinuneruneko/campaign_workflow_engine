package router

import (
	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/handler"
	"gorm.io/gorm"
)

func RegisterRoutes(r *gin.Engine, db *gorm.DB) {
	campaigns := handler.NewCampaignHandler(db)
	workerHandler := handler.NewWorkerHandler(db)

	api := r.Group("/api/v1")
	{
		api.POST("/campaigns", campaigns.CreateCampaign)
		api.GET("/campaigns", campaigns.ListCampaigns)
		api.GET("/campaigns/:id", campaigns.GetCampaignByID)

		api.POST("/worker/run-once", workerHandler.RunOnce)
	}
}
