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
		api.POST("/campaigns/:id/submit", campaigns.SubmitCampaign)
		api.POST("/campaigns/:id/approve", campaigns.ApproveCampaign)
		api.POST("/campaigns/:id/cancel", campaigns.CancelCampaign)
		api.POST("/internal/worker/run-once", workerHandler.RunOnce)
		api.POST("/workflows/:id/delivery", workerHandler.WorkflowDelivery)
	}
}
