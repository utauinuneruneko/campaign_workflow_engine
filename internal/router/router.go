package router

import (
	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/handler"
	"gorm.io/gorm"
)

func RegisterRoutes(r *gin.Engine, db *gorm.DB) {
	campaigns := handler.NewCampaignHandler(db)

	api := r.Group("/api/v1")
	{
		// 注意：这里是 api.POST，不是 r.POST
		api.POST("/campaigns", campaigns.CreateCampaign)
		api.GET("/campaigns", campaigns.ListCampaigns)
		api.GET("/campaigns/:id", campaigns.GetCampaignByID)
	}

}
