package service

import (
	"errors"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal"
	"github.com/utauinuneruneko/campaign_workflow_engine/models"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCalculateNextWorkflowBudget(t *testing.T) {
	day := 24 * time.Hour
	tests := []struct {
		name             string
		total, remaining time.Duration
		budget           int64
		current          time.Duration
		want             int64
		wantErr          bool
	}{
		{"exact seven days", 7 * day, 7 * day, 70000, 7 * day, 70000, false},
		{"non-multiple campaign frontloads remainder", 15 * day, 15 * day, 1500000, 8 * day, 800000, false},
		{"last workflow receives rounding remainder", 15 * day, 7 * day, 700001, 7 * day, 700001, false},
		{"integer currency rounds down proportionally", 3 * day, 3 * day, 10, day, 3, false},
		{"reject zero duration", 0, day, 1, day, 0, true},
		{"reject remaining over total", 7 * day, 8 * day, 1, 7 * day, 0, true},
		{"reject current over remaining", 14 * day, 7 * day, 1, 8 * day, 0, true},
		{"reject negative budget", 7 * day, 7 * day, -1, 7 * day, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateNextWorkflowBudget(tt.total, tt.remaining, tt.budget, tt.current)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
	}
}

func setupWorkerTest(t *testing.T, now time.Time) (*Worker, *internal.FakeClock, *gorm.DB) {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "worker.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.Campaign{}, &models.WorkflowTask{}); err != nil {
		t.Fatal(err)
	}
	fake := internal.NewFakeClock(now)
	return NewWorker(database, fake), fake, database
}

func insertApprovedCampaign(t *testing.T, db *gorm.DB, approvedAt, start, end time.Time) models.Campaign {
	t.Helper()
	campaign := models.Campaign{
		Name: "clock test", StartTime: start, EndTime: end,
		Status: models.CampaignStatusApproved, ApprovedAt: &approvedAt,
		Budget: 1_500_000,
	}
	if err := db.Create(&campaign).Error; err != nil {
		t.Fatal(err)
	}
	return campaign
}

func TestApprovedCampaignBeforeStartDoesNotCreateWorkflow(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w, _, db := setupWorkerTest(t, now)
	campaign := insertApprovedCampaign(t, db, now, now.Add(time.Hour), now.Add(16*24*time.Hour))
	if err := w.RunOnce(); !errors.Is(err, ErrNoPendingTask) {
		t.Fatalf("RunOnce err=%v, want no pending task", err)
	}
	var count int64
	if err := db.Model(&models.WorkflowTask{}).Where("campaign_id = ?", campaign.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("got %d workflow tasks before start, want 0", count)
	}
}

func TestCampaignCreatesFirstWorkflowWhenFakeTimeReachesStart(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w, fake, db := setupWorkerTest(t, now)
	start := now.Add(time.Hour)
	campaign := insertApprovedCampaign(t, db, now, start, start.Add(15*24*time.Hour))
	if err := w.RunOnce(); !errors.Is(err, ErrNoPendingTask) {
		t.Fatalf("before start RunOnce err=%v", err)
	}
	fake.Set(start)
	if err := w.RunOnce(); err != nil {
		t.Fatalf("at start RunOnce: %v", err)
	}
	var task models.WorkflowTask
	if err := db.Where("campaign_id = ?", campaign.ID).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != models.WorkflowTaskStatusRunning {
		t.Fatalf("task status=%v, want RUNNING", task.Status)
	}
	if !task.StartTime.Equal(start) || !task.EndTime.Equal(start.Add(8*24*time.Hour)) {
		t.Fatalf("unexpected first workflow period: %v - %v", task.StartTime, task.EndTime)
	}
	if task.Budget != 800_000 {
		t.Fatalf("first task budget=%d, want 800000", task.Budget)
	}
}

func TestNextWorkflowWaitsForPreviousTerminalState(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w, fake, db := setupWorkerTest(t, now)
	start := now
	campaign := insertApprovedCampaign(t, db, now, start, start.Add(15*24*time.Hour))
	if err := w.RunOnce(); err != nil {
		t.Fatal(err)
	}
	var first models.WorkflowTask
	if err := db.Where("campaign_id = ?", campaign.ID).First(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := w.RunOnce(); !errors.Is(err, ErrNoPendingTask) {
		t.Fatalf("second RunOnce while first runs = %v, want no task", err)
	}
	var count int64
	db.Model(&models.WorkflowTask{}).Where("campaign_id = ?", campaign.ID).Count(&count)
	if count != 1 {
		t.Fatalf("tasks=%d before first terminal, want 1", count)
	}

	if err := db.Model(&models.WorkflowTask{}).Where("id = ?", first.ID).Update("status", models.WorkflowTaskStatusSuccess).Error; err != nil {
		t.Fatal(err)
	}
	fake.Set(first.EndTime.Add(-time.Nanosecond))
	if err := w.RunOnce(); !errors.Is(err, ErrNoPendingTask) {
		t.Fatalf("before next start RunOnce=%v, want no task", err)
	}
	fake.Set(first.EndTime)
	if err := w.RunOnce(); err != nil {
		t.Fatalf("at next start RunOnce: %v", err)
	}
	db.Model(&models.WorkflowTask{}).Where("campaign_id = ?", campaign.ID).Count(&count)
	if count != 2 {
		t.Fatalf("tasks=%d after first terminal and next start, want 2", count)
	}
	var second models.WorkflowTask
	if err := db.Where("campaign_id = ?", campaign.ID).Order("id DESC").First(&second).Error; err != nil {
		t.Fatal(err)
	}
	if second.Status != models.WorkflowTaskStatusRunning {
		t.Fatalf("second status=%v, want RUNNING", second.Status)
	}
}

func TestCampaignCompletesAfterEndTime(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w, fake, db := setupWorkerTest(t, now)
	end := now.Add(15 * 24 * time.Hour)
	campaign := insertApprovedCampaign(t, db, now, now, end)
	if err := w.RunOnce(); err != nil {
		t.Fatal(err)
	}
	fake.Set(end)
	if err := w.RunOnce(); !errors.Is(err, ErrNoPendingTask) {
		t.Fatalf("RunOnce at end=%v, want no task", err)
	}
	var got models.Campaign
	if err := db.First(&got, campaign.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != models.CampaignStatusCompleted {
		t.Fatalf("campaign status=%v, want COMPLETED", got.Status)
	}
}
