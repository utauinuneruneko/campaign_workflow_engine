package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/model"
	"gorm.io/gorm"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type CampaignHandler struct {
	db *gorm.DB
}

func NewCampaignHandler(db *gorm.DB) *CampaignHandler {
	return &CampaignHandler{db: db}
}

type createCampaignRequest struct {
	Name         string    `json:"name" binding:"required,max=128"`
	AdvertiserID string    `json:"advertiser_id" binding:"required,max=64"`
	Budget       float64   `json:"budget" binding:"required,gt=0"`
	EndTime      time.Time `json:"end_time" binding:"required"`
}

func (h *CampaignHandler) CreateCampaign(c *gin.Context) {
	var req createCampaignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.AdvertiserID = strings.TrimSpace(req.AdvertiserID)
	if req.Name == "" || req.AdvertiserID == "" || !req.EndTime.After(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and advertiser_id must be non-blank and end_time must be in the future"})
		return
	}

	campaign := model.Campaign{
		Name:         req.Name,
		AdvertiserID: req.AdvertiserID,
		Budget:       req.Budget,
		Status:       model.CampaignStatusDraft,
		EndTime:      req.EndTime,
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
	id, ok := campaignID(c)
	if !ok {
		return
	}
	var campaign model.Campaign
	err := h.db.First(&campaign, id).Error
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

// SubmitCampaign transitions a DRAFT campaign to SUBMITTED.
func (h *CampaignHandler) SubmitCampaign(c *gin.Context) {
	h.transitionCampaign(c, model.CampaignStatusDraft, model.CampaignStatusSubmitted, false)
}

// ApproveCampaign transitions SUBMITTED to APPROVED and queues delivery when budget is positive.
func (h *CampaignHandler) ApproveCampaign(c *gin.Context) {
	id, ok := campaignID(c)
	if !ok {
		return
	}
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var campaign model.Campaign
		if err := tx.First(&campaign, id).Error; err != nil {
			return err
		}
		if campaign.Status != model.CampaignStatusSubmitted {
			return errInvalidCampaignTransition
		}
		if err := tx.Model(&campaign).Update("status", model.CampaignStatusApproved).Error; err != nil {
			return err
		}
		if campaign.Budget > 0 {
			task := model.WorkflowTask{
				CampaignID:     campaign.ID,
				TaskType:       model.WorkflowTaskTypeStartDelivery,
				Status:         model.WorkflowTaskStatusPending,
				IdempotencyKey: "campaign-start-delivery-" + strconv.FormatUint(uint64(campaign.ID), 10),
			}
			if err := tx.Create(&task).Error; err != nil {
				return err
			}
		}
		return nil
	})
	h.respondTransition(c, err, "campaign approved")
}

// CancelCampaign cancels a campaign only while it is DRAFT or SUBMITTED.
func (h *CampaignHandler) CancelCampaign(c *gin.Context) {
	id, ok := campaignID(c)
	if !ok {
		return
	}
	var campaign model.Campaign
	if err := h.db.First(&campaign, id).Error; err != nil {
		h.respondCampaignLookupError(c, err)
		return
	}
	if campaign.Status != model.CampaignStatusDraft && campaign.Status != model.CampaignStatusSubmitted {
		c.JSON(http.StatusConflict, gin.H{"error": "campaign cannot be cancelled from its current status"})
		return
	}
	h.transitionCampaign(c, campaign.Status, model.CampaignStatusCancelled, false)
}

var errInvalidCampaignTransition = errors.New("invalid campaign status transition")

func (h *CampaignHandler) transitionCampaign(c *gin.Context, from, to model.CampaignStatus, _ bool) {
	id, ok := campaignID(c)
	if !ok {
		return
	}
	result := h.db.Model(&model.Campaign{}).
		Where("id = ? AND status = ?", id, from).
		Update("status", to)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update campaign status"})
		return
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := h.db.Model(&model.Campaign{}).Where("id = ?", id).Count(&count).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to find campaign"})
			return
		}
		if count == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found"})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": "campaign cannot make the requested status transition"})
		return
	}
	h.respondTransition(c, nil, "campaign status updated")
}

func (h *CampaignHandler) respondTransition(c *gin.Context, err error, message string) {
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"message": message})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found"})
		return
	}
	if errors.Is(err, errInvalidCampaignTransition) {
		c.JSON(http.StatusConflict, gin.H{"error": "campaign cannot make the requested status transition"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update campaign status"})
}

func (h *CampaignHandler) respondCampaignLookupError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to find campaign"})
}

func campaignID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid campaign ID"})
		return 0, false
	}
	return uint(id), true
}
