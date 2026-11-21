package worker

import (
	"errors"
	"fmt"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/model"
	"gorm.io/gorm"
	"math/big"
	"time"
)

const WorkflowPeriod = 7 * 24 * time.Hour

var ErrNoPendingTask = errors.New("no eligible pending task")

// CalculateNextWorkflowBudget 使用 big.Int 确保大额预算计算时不溢出，并按比例分配预算。
func CalculateNextWorkflowBudget(totalCampaignDuration, remainingDuration time.Duration, remainingBudget int64, currentWorkflowDuration time.Duration) (int64, error) {
	if totalCampaignDuration <= 0 {
		return 0, errors.New("total campaign duration must be positive")
	}
	if remainingDuration <= 0 || remainingDuration > totalCampaignDuration {
		return 0, errors.New("remaining duration must be positive and no greater than total campaign duration")
	}
	if currentWorkflowDuration <= 0 || currentWorkflowDuration > remainingDuration {
		return 0, errors.New("current workflow duration must be positive and no greater than remaining duration")
	}
	if remainingBudget < 0 {
		return 0, errors.New("remaining budget cannot be negative")
	}
	// 最后一个任务，直接返回剩余所有预算，避免浮点精度损失
	if currentWorkflowDuration == remainingDuration {
		return remainingBudget, nil
	}

	// 使用 big.Int 进行计算: (remainingBudget * currentDuration) / remainingDuration
	n := new(big.Int).Mul(big.NewInt(remainingBudget), big.NewInt(int64(currentWorkflowDuration)))
	n.Quo(n, big.NewInt(int64(remainingDuration)))

	if !n.IsInt64() {
		return 0, errors.New("calculated budget overflows int64")
	}
	return n.Int64(), nil
}

// RunWorkerOnce 驱动工作流流转的主函数
func RunWorkerOnce(db *gorm.DB) error {
	var targetTask model.WorkflowTask
	taskFound := false

	err := db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()

		// 1. 自动完成过期活动
		if err := tx.Model(&model.Campaign{}).
			Where("status = ? AND end_time <= ?", model.CampaignStatusDelivering, now).
			Update("status", model.CampaignStatusCompleted).Error; err != nil {
			return fmt.Errorf("complete expired campaigns: %w", err)
		}

		// 2. 尝试为符合条件的活动创建下一个 Workflow 任务
		newTask, created, err := tryCreateNextTask(tx, now)
		if err != nil {
			return fmt.Errorf("try create next task: %w", err)
		}

		if created {
			targetTask = *newTask
			taskFound = true
		} else {
			// 3. 寻找已经存在但尚未开始的到期任务
			dueTask, found, err := findDueTask(tx, now)
			if err != nil {
				return fmt.Errorf("find due task: %w", err)
			}
			if !found {
				return nil // 既没新任务创建，也没到期任务
			}
			targetTask = *dueTask
			taskFound = true
		}

		// 4. 认领任务并更新活动状态
		return claimTask(tx, targetTask, now)
	})

	if err != nil {
		return err
	}
	if !taskFound {
		return ErrNoPendingTask
	}
	return nil
}

func tryCreateNextTask(tx *gorm.DB, now time.Time) (*model.WorkflowTask, bool, error) {
	var campaigns []model.Campaign
	// 查找状态为已审核或投递中，且尚未结束的活动
	err := tx.Where("status IN ? AND budget > ? AND end_time > ?",
		[]model.CampaignStatus{model.CampaignStatusApproved, model.CampaignStatusDelivering},
		0, now).
		Order("approved_at ASC, id ASC").
		Find(&campaigns).Error
	if err != nil {
		return nil, false, err
	}

	for _, cp := range campaigns {
		if cp.ApprovedAt == nil || cp.ApprovedAt.After(now) {
			continue
		}

		var prev model.WorkflowTask
		err := tx.Where("campaign_id = ?", cp.ID).Order("start_time DESC, id DESC").First(&prev).Error

		var startAt time.Time
		var duration time.Duration
		isFirst := errors.Is(err, gorm.ErrRecordNotFound)

		if isFirst {
			if cp.StartTime.After(now) {
				continue
			}
			startAt = cp.StartTime
			// 首个任务时长 = 7天 + 总时长%7天的余数 (处理非整除情况)
			totalLen := cp.EndTime.Sub(cp.StartTime)
			duration = WorkflowPeriod + (totalLen % WorkflowPeriod)
		} else if err == nil {
			// 只有前一个任务成功或失败了（终态），才开启下一个
			if prev.Status != model.WorkflowTaskStatusSuccess && prev.Status != model.WorkflowTaskStatusFailed {
				continue
			}
			startAt = prev.EndTime
			if startAt.After(now) || !startAt.Before(cp.EndTime) {
				continue
			}
			duration = WorkflowPeriod
		} else {
			return nil, false, err
		}

		// 修正时长，确保不超限
		remLen := cp.EndTime.Sub(startAt)
		if duration > remLen {
			duration = remLen
		}
		if duration <= 0 {
			continue
		}

		// 计算该阶段应分配的预算
		allocated, err := calcBudget(tx, cp, startAt, duration, isFirst)
		if err != nil {
			return nil, false, err
		}

		var seq int64
		tx.Model(&model.WorkflowTask{}).Where("campaign_id = ?", cp.ID).Count(&seq)

		next := model.WorkflowTask{
			CampaignID:     cp.ID,
			Status:         model.WorkflowTaskStatusPending,
			Budget:         allocated,
			StartTime:      startAt,
			EndTime:        startAt.Add(duration),
			IdempotencyKey: fmt.Sprintf("cp-%d-task-%d", cp.ID, seq+1),
		}

		if err := tx.Create(&next).Error; err != nil {
			return nil, false, err
		}
		return &next, true, nil
	}
	return nil, false, nil
}

func calcBudget(tx *gorm.DB, cp model.Campaign, startAt time.Time, dur time.Duration, isFirst bool) (int64, error) {
	totalDur := cp.EndTime.Sub(cp.StartTime)
	remDur := cp.EndTime.Sub(startAt)

	remBudget := cp.Budget
	if !isFirst {
		var spent int64
		// 统计该活动下已分配出去的所有任务预算总和
		if err := tx.Model(&model.WorkflowTask{}).
			Where("campaign_id = ?", cp.ID).
			Select("COALESCE(SUM(budget), 0)").
			Scan(&spent).Error; err != nil {
			return 0, err
		}
		remBudget = cp.Budget - spent
	}

	return CalculateNextWorkflowBudget(totalDur, remDur, remBudget, dur)
}

func findDueTask(tx *gorm.DB, now time.Time) (*model.WorkflowTask, bool, error) {
	var t model.WorkflowTask
	err := tx.Joins("JOIN campaigns ON campaigns.id = workflow_tasks.campaign_id").
		Where("workflow_tasks.status = ? AND workflow_tasks.start_time <= ? AND campaigns.status IN ?",
			model.WorkflowTaskStatusPending, now,
			[]model.CampaignStatus{model.CampaignStatusApproved, model.CampaignStatusDelivering}).
		Order("workflow_tasks.create_time ASC").
		First(&t).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return &t, err == nil, err
}

func claimTask(tx *gorm.DB, task model.WorkflowTask, now time.Time) error {
	// 原子更新任务状态为运行中
	res := tx.Model(&model.WorkflowTask{}).
		Where("id = ? AND status = ?", task.ID, model.WorkflowTaskStatusPending).
		Update("status", model.WorkflowTaskStatusRunning)

	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return nil
	}

	// 如果活动还是“已审核”状态，说明这是第一个开始跑的任务，将其转为“投递中”
	return tx.Model(&model.Campaign{}).
		Where("id = ? AND status = ?", task.CampaignID, model.CampaignStatusApproved).
		Update("status", model.CampaignStatusDelivering).Error
}
