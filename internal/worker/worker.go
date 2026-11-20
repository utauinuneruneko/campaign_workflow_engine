package worker

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"campaign_workflow_engine/internal/model"
	"gorm.io/gorm"
)

const WorkflowPeriod = 7 * 24 * time.Hour
var ErrNoPendingTask = errors.New("no eligible pending task")

// CalculateNextWorkflowBudget allocates integer currency units proportionally.
// The final workflow receives the full remainder to eliminate rounding loss.
func CalculateNextWorkflowBudget(totalCampaignDuration, remainingDuration time.Duration, remainingBudget int64, currentWorkflowDuration time.Duration) (int64,error) {
	if totalCampaignDuration<=0{return 0,errors.New("total campaign duration must be positive")}
	if remainingDuration<=0||remainingDuration>totalCampaignDuration{return 0,errors.New("remaining duration must be positive and no greater than total campaign duration")}
	if currentWorkflowDuration<=0||currentWorkflowDuration>remainingDuration{return 0,errors.New("current workflow duration must be positive and no greater than remaining duration")}
	if remainingBudget<0{return 0,errors.New("remaining budget cannot be negative")}
	if currentWorkflowDuration==remainingDuration{return remainingBudget,nil}
	n:=new(big.Int).Mul(big.NewInt(remainingBudget),big.NewInt(int64(currentWorkflowDuration)))
	n.Quo(n,big.NewInt(int64(remainingDuration)))
	if !n.IsInt64(){return 0,errors.New("calculated budget overflows int64")}
	return n.Int64(),nil
}

// RunWorkerOnce completes expired campaign delivery, creates the next workflow
// sequentially after the prior task is terminal, and claims one due workflow.
func RunWorkerOnce(db *gorm.DB) error {
	var task model.WorkflowTask;found:=false
	err:=db.Transaction(func(tx *gorm.DB)error{
		now:=time.Now()
		if err:=tx.Model(&model.Campaign{}).Where("status = ? AND end_time <= ?",model.CampaignStatusDelivering,now).Update("status",model.CampaignStatusCompleted).Error;err!=nil{return fmt.Errorf("complete expired campaigns: %w",err)}
		campaigns:=make([]model.Campaign,0)
		if err:=tx.Where("status IN ? AND budget > ? AND end_time > ?",[]model.CampaignStatus{model.CampaignStatusApproved,model.CampaignStatusDelivering},0,now).Order("approved_at ASC").Order("id ASC").Find(&campaigns).Error;err!=nil{return fmt.Errorf("find eligible campaigns: %w",err)}
		for _,campaign:=range campaigns{
			if campaign.ApprovedAt==nil||campaign.ApprovedAt.After(now){continue}
			var previous model.WorkflowTask
			prevErr:=tx.Where("campaign_id = ?",campaign.ID).Order("start_at DESC").Order("id DESC").First(&previous).Error
			startAt:=campaign.StartTime;duration:=campaign.EndTime.Sub(startAt)
			if errors.Is(prevErr,gorm.ErrRecordNotFound){if startAt.After(now){continue};duration=WorkflowPeriod+duration%WorkflowPeriod
			}else if prevErr!=nil{return fmt.Errorf("find previous workflow: %w",prevErr)}else{
				if previous.Status!=model.WorkflowTaskStatusSuccess&&previous.Status!=model.WorkflowTaskStatusFailed{continue}
				startAt=previous.EndAt;if startAt.After(now)||!startAt.Before(campaign.EndTime){continue};duration=WorkflowPeriod
			}
			remainingDuration:=campaign.EndTime.Sub(startAt);if duration>remainingDuration{duration=remainingDuration};if duration<=0{continue}
			var allocated int64
			if errors.Is(prevErr,gorm.ErrRecordNotFound){allocated,err=CalculateNextWorkflowBudget(campaign.EndTime.Sub(campaign.StartTime),remainingDuration,campaign.Budget,duration)}else{
				var allocatedBudget int64
				if err:=tx.Model(&model.WorkflowTask{}).Where("campaign_id = ?",campaign.ID).Select("COALESCE(SUM(budget),0)").Scan(&allocatedBudget).Error;err!=nil{return fmt.Errorf("sum allocated budgets: %w",err)}
				allocated,err=CalculateNextWorkflowBudget(campaign.EndTime.Sub(campaign.StartTime),remainingDuration,campaign.Budget-allocatedBudget,duration)
			};if err!=nil{return fmt.Errorf("calculate workflow budget: %w",err)}
			var sequence int64;if err:=tx.Model(&model.WorkflowTask{}).Where("campaign_id = ?",campaign.ID).Count(&sequence).Error;err!=nil{return err}
			next:=model.WorkflowTask{CampaignID:campaign.ID,Status:model.WorkflowTaskStatusPending,Type:model.WorkflowTaskTypeStartDelivery,Budget:allocated,StartAt:startAt,EndAt:startAt.Add(duration),IdempotencyKey:fmt.Sprintf("campaign-%d-delivery-%d",campaign.ID,sequence+1)}
			if err:=tx.Create(&next).Error;err!=nil{return fmt.Errorf("create next workflow: %w",err)};task=next;found=true;break
		}
		if !found{
			err:=tx.Joins("JOIN campaigns ON campaigns.id = workflow_tasks.campaign_id").Where("workflow_tasks.status = ? AND workflow_tasks.start_at <= ? AND workflow_tasks.end_at > ? AND campaigns.status IN ? AND campaigns.end_time > ?",model.WorkflowTaskStatusPending,now,now,[]model.CampaignStatus{model.CampaignStatusApproved,model.CampaignStatusDelivering},now).Order("workflow_tasks.create_time ASC").Order("workflow_tasks.id ASC").First(&task).Error
			if errors.Is(err,gorm.ErrRecordNotFound){return nil};if err!=nil{return fmt.Errorf("find due workflow: %w",err)};found=true
		}
		claim:=tx.Model(&model.WorkflowTask{}).Where("id = ? AND status = ? AND start_at <= ?",task.ID,model.WorkflowTaskStatusPending,now).Update("status",model.WorkflowTaskStatusRunning)
		if claim.Error!=nil{return claim.Error};if claim.RowsAffected==0{found=false;return nil}
		if err:=tx.Model(&model.Campaign{}).Where("id = ? AND status = ?",task.CampaignID,model.CampaignStatusApproved).Update("status",model.CampaignStatusDelivering).Error;err!=nil{return err}
		return nil
	})
	if err!=nil{return err};if !found{return ErrNoPendingTask};return nil
}
