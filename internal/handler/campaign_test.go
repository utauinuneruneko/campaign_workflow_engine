package handler

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestCreateCampaign(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "campaign-test.db")
	database, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	if err := database.AutoMigrate(&model.Campaign{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	r := gin.New()
	campaignHandler := NewCampaignHandler(database)
	r.POST("/campaigns", campaignHandler.CreateCampaign)

	requestBody := []byte(`{
		"name": "Test Campaign",
		"advertiser_id": "advertiser-123",
		"budget": 1000
	}`)
	req := httptest.NewRequest(http.MethodPost, "/campaigns", bytes.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s",
			http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response model.Campaign
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if response.Status != "DRAFT" {
		t.Errorf("expected campaign status %q, got %q", "DRAFT", response.Status)
	}
}
