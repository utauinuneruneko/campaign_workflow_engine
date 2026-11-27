package repo

import (
	"fmt"

	"github.com/utauinuneruneko/campaign_workflow_engine/models"
	"gorm.io/gorm"
)

// WorkflowRepository owns persistence operations for workflow tasks.
type WorkflowRepository struct {
	db *gorm.DB
}

func NewWorkflowRepository(db *gorm.DB) *WorkflowRepository {
	return &WorkflowRepository{db: db}
}

// WithDB binds this repository to another handle, such as a transaction.
func (r *WorkflowRepository) WithDB(db *gorm.DB) *WorkflowRepository {
	return NewWorkflowRepository(db)
}

func (r *WorkflowRepository) Create(task *models.WorkflowTask) error {
	if err := r.db.Create(task).Error; err != nil {
		return fmt.Errorf("create workflow task: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) List() ([]models.WorkflowTask, error) {
	var tasks []models.WorkflowTask
	if err := r.db.Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("list workflow tasks: %w", err)
	}
	return tasks, nil
}

func (r *WorkflowRepository) GetByID(id int64) (models.WorkflowTask, error) {
	var task models.WorkflowTask
	if err := r.db.First(&task, id).Error; err != nil {
		return models.WorkflowTask{}, err
	}
	return task, nil
}

// UpdateByID intentionally uses only the primary key as a database condition.
// Callers load and validate task state before applying updates.
func (r *WorkflowRepository) UpdateByID(
	id int64,
	updates map[string]interface{},
) (int64, error) {
	result := r.db.Model(&models.WorkflowTask{}).
		Where("id = ?", id).
		Updates(updates)
	if result.Error != nil {
		return 0, fmt.Errorf("update workflow task %d: %w", id, result.Error)
	}
	return result.RowsAffected, nil
}

func (r *WorkflowRepository) DeleteByID(id int64) error {
	if err := r.db.Delete(&models.WorkflowTask{}, id).Error; err != nil {
		return fmt.Errorf("delete workflow task %d: %w", id, err)
	}
	return nil
}
