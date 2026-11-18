package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/model"
	"gorm.io/gorm"
	"net/http"
	"strconv"
	"strings"
)

type CampaignHandler struct {
	db *gorm.DB
}

func NewCampaignHandler(db *gorm.DB) *CampaignHandler {
	return &CampaignHandler{db: db}
}

type createCampaignRequest struct {
	Name         string  `json:"name" binding:"required,max=128"`
	AdvertiserID string  `json:"advertiser_id" binding:"required,max=64"`
	Budget       float64 `json:"budget" binding:"required,gt=0"`
}

func (h *CampaignHandler) CreateCampaign(c *gin.Context) {
	var req createCampaignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.AdvertiserID = strings.TrimSpace(req.AdvertiserID)
	if req.Name == "" || req.AdvertiserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and advertiser_id must not be blank"})
		return
	}

	campaign := model.Campaign{
		Name:         req.Name,
		AdvertiserID: req.AdvertiserID,
		Budget:       req.Budget,
		Status:       "DRAFT",
	}

	if err := h.db.Create(&campaign).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create campaign"})
		return
	}

	c.JSON(http.StatusCreated, campaign)
}

func (h *CampaignHandler) ListCampaigns(c *gin.Context) {
	var campaigns []model.Campaign
	if err := h.db.Find(&campaigns).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list campaigns"})
		return
	}

	c.JSON(http.StatusOK, campaigns)
}

func (h *CampaignHandler) GetCampaignByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid campaign ID"})
		return
	}

	var campaign model.Campaign
	err = h.db.First(&campaign, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch campaign"})
		return
	}

	c.JSON(http.StatusOK, campaign)
}
