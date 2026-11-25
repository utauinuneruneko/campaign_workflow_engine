package service

import (
	"context"
	"errors"
	"github.com/utauinuneruneko/campaign_workflow_engine/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

func setupCampaignServiceTest(t *testing.T, now time.Time) (*CampaignService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "service.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Campaign{}, &models.WorkflowTask{}); err != nil {
		t.Fatal(err)
	}
	svc := NewCampaignService(db)
	svc.now = func() time.Time { return now }
	return svc, db
}
func TestSubmitOnlyDraft(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc, db := setupCampaignServiceTest(t, now)
	campaign := models.Campaign{Name: "campaign", StartTime: now.Add(-time.Hour), EndTime: now.Add(14 * 24 * time.Hour), Status: models.CampaignStatusDraft, Budget: 10000000}
	if err := db.Create(&campaign).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Submit(context.Background(), campaign.ID); err != nil {
		t.Fatal(err)
	}
	var got models.Campaign
	if err := db.First(&got, campaign.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != models.CampaignStatusSubmitted || got.SubmittedAt == nil {
		t.Fatalf("unexpected campaign: %+v", got)
	}
}
func TestApproveBeforeStartCreatesTaskOnlyWhenDue(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc, db := setupCampaignServiceTest(t, now)
	campaign := models.Campaign{Name: "campaign", StartTime: now.Add(time.Hour), EndTime: now.Add(8 * 24 * time.Hour), Status: models.CampaignStatusSubmitted, Budget: 5000000, SubmittedAt: timePtr(now)}
	if err := db.Create(&campaign).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(context.Background(), campaign.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.WorkflowTask{}).Where("campaign_id = ?", campaign.ID).Count(&count)
	if count != 0 {
		t.Fatalf("got %d tasks before start", count)
	}
	if err := svc.CreateInitialTaskIfDue(context.Background(), campaign.ID); !errors.Is(err, ErrCampaignNotStarted) {
		t.Fatalf("got %v want ErrCampaignNotStarted", err)
	}
	svc.now = func() time.Time { return now.Add(time.Hour) }
	if err := svc.CreateInitialTaskIfDue(context.Background(), campaign.ID); err != nil {
		t.Fatal(err)
	}
	db.Model(&models.WorkflowTask{}).Where("campaign_id = ?", campaign.ID).Count(&count)
	if count != 1 {
		t.Fatalf("got %d tasks want 1", count)
	}
}
func TestExpiredSubmissionRevertsOnApproval(t *testing.T) {
	now := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)
	svc, db := setupCampaignServiceTest(t, now)
	campaign := models.Campaign{Name: "late", StartTime: now, EndTime: now.Add(8 * 24 * time.Hour), Status: models.CampaignStatusSubmitted, Budget: 1000000, SubmittedAt: timePtr(now.Add(-SubmissionExpiry - time.Second))}
	if err := db.Create(&campaign).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(context.Background(), campaign.ID); !errors.Is(err, ErrSubmissionExpired) {
		t.Fatalf("got %v want expired", err)
	}
	var got models.Campaign
	if err := db.First(&got, campaign.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != models.CampaignStatusDraft {
		t.Fatalf("got status %v want DRAFT", got.Status)
	}
}
func timePtr(t time.Time) *time.Time { return &t }
