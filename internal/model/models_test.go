package model

import (
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)
func TestCampaignDurationValidation(t *testing.T){db,err:=gorm.Open(sqlite.Open(filepath.Join(t.TempDir(),"model.db")),&gorm.Config{});if err!=nil{t.Fatal(err)};if err:=db.AutoMigrate(&Campaign{});err!=nil{t.Fatal(err)};now:=time.Now();for _,tc:=range []struct{name string;duration time.Duration;wantErr bool}{{"seven days",MinimumCampaignDuration,false},{"shorter",MinimumCampaignDuration-time.Second,true}}{t.Run(tc.name,func(t *testing.T){c:=Campaign{Name:"test",StartTime:now,EndTime:now.Add(tc.duration),Status:CampaignStatusDraft};err:=db.Create(&c).Error;if (err!=nil)!=tc.wantErr{t.Fatalf("Create err=%v wantErr=%v",err,tc.wantErr)}})}}
func TestStatusesUseSQLiteTinyIntColumns(t *testing.T){db,err:=gorm.Open(sqlite.Open(filepath.Join(t.TempDir(),"types.db")),&gorm.Config{});if err!=nil{t.Fatal(err)};if err:=db.AutoMigrate(&Campaign{},&WorkflowTask{});err!=nil{t.Fatal(err)};for _,table:=range []string{"campaigns","workflow_tasks"}{var rows []struct{Name string;Type string};if err:=db.Raw("PRAGMA table_info("+table+")").Scan(&rows).Error;err!=nil{t.Fatal(err)};found:=false;for _,row:=range rows{if row.Name=="status"{found=true;if row.Type!="TINYINT"{t.Fatalf("%s.status type=%q want TINYINT",table,row.Type)}}};if !found{t.Fatalf("%s.status not found",table)}}}
