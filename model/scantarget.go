package model

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ScanTargetModel provides database operations for ScanTarget
type ScanTargetModel struct {
	*BaseModel[ScanTarget]
}

// NewScanTargetModel creates a new ScanTargetModel
func NewScanTargetModel(db *mongo.Database, workspaceId string) *ScanTargetModel {
	coll := db.Collection(workspaceId + "_scantarget")

	// Create indexes
	ctx := context.Background()
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "job_id", Value: 1}}},
		{Keys: bson.D{{Key: "host", Value: 1}}},
		{Keys: bson.D{{Key: "category", Value: 1}}},
		{Keys: bson.D{{Key: "priority", Value: -1}}},
		{Keys: bson.D{{Key: "create_time", Value: -1}}},
	}
	coll.Indexes().CreateMany(ctx, indexes)

	return &ScanTargetModel{
		BaseModel: NewBaseModel[ScanTarget](coll),
	}
}

// FindByJobID finds scan targets by job ID
func (m *ScanTargetModel) FindByJobID(ctx context.Context, jobID string) ([]ScanTarget, error) {
	return m.FindWithSort(ctx, bson.M{"job_id": jobID}, 0, 0, "priority", -1)
}

// FindByJobIDWithPage finds scan targets by job ID with pagination
func (m *ScanTargetModel) FindByJobIDWithPage(ctx context.Context, jobID string, params PageParams) (*PageResult[ScanTarget], error) {
	return m.FindWithPage(ctx, bson.M{"job_id": jobID}, params)
}

// FindByHost finds scan targets by host
func (m *ScanTargetModel) FindByHost(ctx context.Context, host string) ([]ScanTarget, error) {
	return m.FindWithSort(ctx, bson.M{"host": host}, 0, 0, "create_time", -1)
}

// FindByCategory finds scan targets by category
func (m *ScanTargetModel) FindByCategory(ctx context.Context, category string, params PageParams) (*PageResult[ScanTarget], error) {
	return m.FindWithPage(ctx, bson.M{"category": category}, params)
}

// FindByPriority finds scan targets by priority range
func (m *ScanTargetModel) FindByPriority(ctx context.Context, minPriority, maxPriority int, params PageParams) (*PageResult[ScanTarget], error) {
	filter := bson.M{
		"priority": bson.M{
			"$gte": minPriority,
			"$lte": maxPriority,
		},
	}
	return m.FindWithPage(ctx, filter, params)
}

// CountByJobID counts scan targets by job ID
func (m *ScanTargetModel) CountByJobID(ctx context.Context, jobID string) (int64, error) {
	return m.Count(ctx, bson.M{"job_id": jobID})
}

// DeleteByJobID deletes all scan targets for a job
func (m *ScanTargetModel) DeleteByJobID(ctx context.Context, jobID string) (int64, error) {
	return m.DeleteMany(ctx, bson.M{"job_id": jobID})
}

// BulkInsert inserts multiple scan targets efficiently
func (m *ScanTargetModel) BulkInsert(ctx context.Context, targets []ScanTarget) error {
	if len(targets) == 0 {
		return nil
	}

	// Prepare documents
	docs := make([]interface{}, len(targets))
	for i, target := range targets {
		m.PrepareDocument(&target)
		docs[i] = target
	}

	_, err := m.Coll.InsertMany(ctx, docs)
	return err
}
