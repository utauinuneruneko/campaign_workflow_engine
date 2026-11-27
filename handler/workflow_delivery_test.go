package handler

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/utauinuneruneko/campaign_workflow_engine/models"
	"github.com/utauinuneruneko/campaign_workflow_engine/repo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRecordWorkflowSpendSetsActualEndTimeWhenSuccessful(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "workflow-delivery.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.WorkflowTask{}); err != nil {
		t.Fatal(err)
	}

	scheduledEnd := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	task := models.WorkflowTask{
		CampaignID:     1,
		Status:         models.WorkflowTaskStatusRunning,
		Budget:         500,
		StartTime:      scheduledEnd.Add(-7 * 24 * time.Hour),
		EndTime:        scheduledEnd,
		IdempotencyKey: "campaign-1-delivery-1",
	}
	if err := repo.NewWorkflowRepository(database).Create(&task); err != nil {
		t.Fatal(err)
	}

	actualEnd := scheduledEnd.Add(-2 * time.Hour)
	if err := recordWorkflowSpend(database, task, 500, actualEnd); err != nil {
		t.Fatal(err)
	}

	updated, err := repo.NewWorkflowRepository(database).GetByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != models.WorkflowTaskStatusSuccess {
		t.Fatalf("status=%v, want SUCCESS", updated.Status)
	}
	if updated.ActualEndTime == nil || !updated.ActualEndTime.Equal(actualEnd) {
		t.Fatalf("actual end time=%v, want %v", updated.ActualEndTime, actualEnd)
	}
	if !updated.EndTime.Equal(scheduledEnd) {
		t.Fatalf("scheduled end time=%v, want %v", updated.EndTime, scheduledEnd)
	}
}
