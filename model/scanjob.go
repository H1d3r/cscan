package model

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ScanJobModel provides database operations for ScanJob
type ScanJobModel struct {
	*BaseModel[ScanJob]
}

// NewScanJobModel creates a new ScanJobModel
func NewScanJobModel(db *mongo.Database, workspaceId string) *ScanJobModel {
	coll := db.Collection(workspaceId + "_scanjob")

	// Create indexes
	ctx := context.Background()
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "task_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "create_time", Value: -1}}},
		{Keys: bson.D{{Key: "org_id", Value: 1}}},
	}
	coll.Indexes().CreateMany(ctx, indexes)

	return &ScanJobModel{
		BaseModel: NewBaseModel[ScanJob](coll),
	}
}

// FindByTaskID finds a scan job by task ID
func (m *ScanJobModel) FindByTaskID(ctx context.Context, taskID string) (*ScanJob, error) {
	return m.FindOne(ctx, bson.M{"task_id": taskID})
}

// FindByStatus finds scan jobs by status
func (m *ScanJobModel) FindByStatus(ctx context.Context, status Status, params PageParams) (*PageResult[ScanJob], error) {
	return m.FindWithPage(ctx, bson.M{"status": status}, params)
}

// FindByOrgID finds scan jobs by organization ID
func (m *ScanJobModel) FindByOrgID(ctx context.Context, orgID string, params PageParams) (*PageResult[ScanJob], error) {
	return m.FindWithPage(ctx, bson.M{"org_id": orgID}, params)
}

// UpdateStatus updates the status of a scan job
func (m *ScanJobModel) UpdateStatus(ctx context.Context, id string, status Status) error {
	return m.UpdateById(ctx, id, bson.M{"status": status})
}

// UpdateProgress updates the progress of a scan job
func (m *ScanJobModel) UpdateProgress(ctx context.Context, id string, progress int) error {
	return m.UpdateById(ctx, id, bson.M{"progress": progress})
}

// UpdateState updates the task state of a scan job
func (m *ScanJobModel) UpdateState(ctx context.Context, id string, state TaskState) error {
	return m.UpdateById(ctx, id, bson.M{"state": state})
}

// FindActiveJobs finds all active (non-terminal) scan jobs
func (m *ScanJobModel) FindActiveJobs(ctx context.Context) ([]ScanJob, error) {
	activeStatuses := []Status{StatusCreated, StatusPending, StatusStarted, StatusPaused}
	filter := bson.M{"status": bson.M{"$in": activeStatuses}}
	return m.FindWithSort(ctx, filter, 0, 0, "create_time", -1)
}

// CountByStatus counts scan jobs by status
func (m *ScanJobModel) CountByStatus(ctx context.Context, status Status) (int64, error) {
	return m.Count(ctx, bson.M{"status": status})
}
