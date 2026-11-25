package handler

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/models"
	"gorm.io/gorm"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	errInvalidCampaignTransition = errors.New("invalid campaign status transition")
	errCampaignTermTooShort      = errors.New("campaign term must be at least seven days")
	errCampaignBudgetRequired    = errors.New("campaign budget must be positive")
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
	Budget       int64     `json:"budget" binding:"required,gt=0"`
	StartTime    time.Time `json:"start_time" binding:"required"`
	EndTime      time.Time `json:"end_time" binding:"required"`
}

type campaignWorkflowSummary struct {
	ID        int64                     `json:"id"`
	Status    models.WorkflowTaskStatus `json:"status"`
	StartTime time.Time                 `json:"start_time"`
	EndTime   time.Time                 `json:"end_time"`
	Budget    int64                     `json:"budget"`
}

type campaignDetailResponse struct {
	models.Campaign
	WorkflowTasks []campaignWorkflowSummary `json:"workflow_tasks"`
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
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "name and advertiser_id must not be blank",
		})
		return
	}

	campaign := models.Campaign{
		Name:         req.Name,
		AdvertiserID: req.AdvertiserID,
		Budget:       req.Budget,
		Status:       models.CampaignStatusDraft,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
	}
	if err := h.db.Create(&campaign).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, campaign)
}

func (h *CampaignHandler) ListCampaigns(c *gin.Context) {
	var campaigns []models.Campaign
	if err := h.db.Find(&campaigns).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to list campaigns",
		})
		return
	}
	c.JSON(http.StatusOK, campaigns)
}

func (h *CampaignHandler) GetCampaignByID(c *gin.Context) {
	id, ok := campaignID(c)
	if !ok {
		return
	}

	campaign, err := h.findCampaign(id)
	if err != nil {
		h.respondCampaignLookupError(c, err)
		return
	}

	workflows, err := h.findCampaignWorkflows(campaign.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to fetch campaign workflows",
		})
		return
	}

	c.JSON(http.StatusOK, campaignDetailResponse{
		Campaign:      campaign,
		WorkflowTasks: workflows,
	})
}

func (h *CampaignHandler) findCampaign(id int64) (models.Campaign, error) {
	var campaign models.Campaign
	if err := h.db.First(&campaign, id).Error; err != nil {
		return models.Campaign{}, err
	}
	return campaign, nil
}

func (h *CampaignHandler) findCampaignWorkflows(
	campaignID int64,
) ([]campaignWorkflowSummary, error) {
	var workflows []campaignWorkflowSummary
	err := h.db.Model(&models.WorkflowTask{}).
		Select("id, status, start_time, end_time, budget").
		Where("campaign_id = ?", campaignID).
		Order("start_time ASC").
		Order("id ASC").
		Scan(&workflows).Error
	return workflows, err
}

func (h *CampaignHandler) respondCampaignLookupError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": "failed to fetch campaign",
	})
}

func (h *CampaignHandler) SubmitCampaign(c *gin.Context) {
	h.transitionCampaign(
		c,
		models.CampaignStatusDraft,
		models.CampaignStatusSubmitted,
	)
}

func (h *CampaignHandler) ApproveCampaign(c *gin.Context) {
	id, ok := campaignID(c)
	if !ok {
		return
	}

	now := time.Now()
	err := h.db.Transaction(func(tx *gorm.DB) error {
		campaign, err := h.loadCampaignForTransition(tx, id)
		if err != nil {
			return err
		}
		if err := validateApproval(campaign); err != nil {
			return err
		}
		if err := h.approveCampaign(tx, campaign, now); err != nil {
			return err
		}
		return h.createInitialWorkflowIfDue(tx, campaign, now)
	})
	h.respondCampaignMutation(c, err, "campaign approved")
}

func (h *CampaignHandler) loadCampaignForTransition(
	tx *gorm.DB,
	id int64,
) (models.Campaign, error) {
	var campaign models.Campaign
	if err := tx.First(&campaign, id).Error; err != nil {
		return models.Campaign{}, err
	}
	if campaign.Status != models.CampaignStatusSubmitted {
		return models.Campaign{}, errInvalidCampaignTransition
	}
	return campaign, nil
}

func validateApproval(campaign models.Campaign) error {
	if campaign.Budget <= 0 {
		return errCampaignBudgetRequired
	}
	if campaign.EndTime.Sub(campaign.StartTime) < models.MinimumCampaignDuration {
		return errCampaignTermTooShort
	}
	return nil
}

func (h *CampaignHandler) approveCampaign(
	tx *gorm.DB,
	campaign models.Campaign,
	now time.Time,
) error {
	result := tx.Model(&models.Campaign{}).
		Where("id = ? AND status = ?", campaign.ID, models.CampaignStatusSubmitted).
		Updates(map[string]interface{}{
			"status":      models.CampaignStatusApproved,
			"approved_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errInvalidCampaignTransition
	}
	return nil
}

func (h *CampaignHandler) createInitialWorkflowIfDue(
	tx *gorm.DB,
	campaign models.Campaign,
	now time.Time,
) error {
	if now.Before(campaign.StartTime) {
		return nil
	}

	var count int64
	if err := tx.Model(&models.WorkflowTask{}).
		Where("campaign_id = ?", campaign.ID).
		Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return nil
	}

	workflow := models.WorkflowTask{
		CampaignID:     campaign.ID,
		Status:         models.WorkflowTaskStatusPending,
		Budget:         campaign.Budget,
		StartTime:      campaign.StartTime,
		EndTime:        campaign.EndTime,
		IdempotencyKey: fmt.Sprintf("campaign-%d-initial-workflow", campaign.ID),
	}
	return tx.Create(&workflow).Error
}

func (h *CampaignHandler) CancelCampaign(c *gin.Context) {
	id, ok := campaignID(c)
	if !ok {
		return
	}

	result := h.db.Model(&models.Campaign{}).
		Where(
			"id = ? AND status IN ?",
			id,
			[]models.CampaignStatus{
				models.CampaignStatusDraft,
				models.CampaignStatusSubmitted,
			},
		).
		Update("status", models.CampaignStatusCancelled)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to cancel campaign",
		})
		return
	}
	if result.RowsAffected == 0 {
		h.respondTransitionNotApplied(c, id)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "campaign cancelled"})
}

func (h *CampaignHandler) transitionCampaign(
	c *gin.Context,
	from models.CampaignStatus,
	to models.CampaignStatus,
) {
	id, ok := campaignID(c)
	if !ok {
		return
	}

	updates := map[string]interface{}{"status": to}
	if to == models.CampaignStatusSubmitted {
		updates["submitted_at"] = time.Now()
	}
	result := h.db.Model(&models.Campaign{}).
		Where("id = ? AND status = ?", id, from).
		Updates(updates)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to update campaign status",
		})
		return
	}
	if result.RowsAffected == 0 {
		h.respondTransitionNotApplied(c, id)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "campaign status updated"})
}

func (h *CampaignHandler) respondTransitionNotApplied(c *gin.Context, id int64) {
	var count int64
	if err := h.db.Model(&models.Campaign{}).
		Where("id = ?", id).
		Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to find campaign",
		})
		return
	}
	if count == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found"})
		return
	}
	c.JSON(http.StatusConflict, gin.H{
		"error": "campaign cannot make the requested status transition",
	})
}

func (h *CampaignHandler) respondCampaignMutation(
	c *gin.Context,
	err error,
	message string,
) {
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"message": message})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found"})
	case errors.Is(err, errInvalidCampaignTransition):
		c.JSON(http.StatusConflict, gin.H{
			"error": "campaign cannot make the requested status transition",
		})
	case errors.Is(err, errCampaignBudgetRequired),
		errors.Is(err, errCampaignTermTooShort):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to update campaign",
		})
	}
}

func campaignID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid campaign ID"})
		return 0, false
	}
	return id, true
}
