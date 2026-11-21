package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)
func TestCreateCampaign(t *testing.T){gin.SetMode(gin.TestMode);database,err:=gorm.Open(sqlite.Open(filepath.Join(t.TempDir(),"campaign-test.db")),&gorm.Config{});if err!=nil{t.Fatal(err)};if err:=database.AutoMigrate(&model.Campaign{},&model.WorkflowTask{});err!=nil{t.Fatal(err)};r:=gin.New();h:=NewCampaignHandler(database);r.POST("/campaigns",h.CreateCampaign);now:=time.Now();body,err:=json.Marshal(map[string]interface{}{"name":"Test Campaign","advertiser_id":"advertiser-123","budget":1000*10000,"start_time":now.UTC().Format(time.RFC3339),"end_time":now.Add(8*24*time.Hour).UTC().Format(time.RFC3339)});if err!=nil{t.Fatal(err)};req:=httptest.NewRequest(http.MethodPost,"/campaigns",bytes.NewReader(body));req.Header.Set("Content-Type","application/json");rec:=httptest.NewRecorder();r.ServeHTTP(rec,req);if rec.Code!=http.StatusCreated{t.Fatalf("status=%d body=%s",rec.Code,rec.Body.String())};var got model.Campaign;if err:=json.Unmarshal(rec.Body.Bytes(),&got);err!=nil{t.Fatal(err)};if got.Status!=model.CampaignStatusDraft{t.Fatalf("status=%v want DRAFT",got.Status)};if got.Budget!=1000*10000{t.Fatalf("budget=%d want %d",got.Budget,1000*10000)}}
