package model

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ScanResultModel provides database operations for ScanResult
type ScanResultModel struct {
	*BaseModel[ScanResult]
}

// NewScanResultModel creates a new ScanResultModel
func NewScanResultModel(db *mongo.Database, workspaceId string) *ScanResultModel {
	coll := db.Collection(workspaceId + "_scanresult")

	// Create indexes
	ctx := context.Background()
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "job_id", Value: 1}}},
		{Keys: bson.D{{Key: "target_id", Value: 1}}},
		{Keys: bson.D{{Key: "risk_score", Value: -1}}},
		{Keys: bson.D{{Key: "risk_level", Value: 1}}},
		{Keys: bson.D{{Key: "completed", Value: -1}}},
		{Keys: bson.D{{Key: "create_time", Value: -1}}},
		// New composite index for efficient scan result queries
		{Keys: bson.D{
			{Key: "workspace_id", Value: 1},
			{Key: "authority", Value: 1},
			{Key: "host", Value: 1},
			{Key: "port", Value: 1},
			{Key: "scan_time", Value: -1},
		}},
		// Index for scan_time to support versioning queries
		{Keys: bson.D{{Key: "scan_time", Value: -1}}},
		// Index for version to support versioning queries
		{Keys: bson.D{{Key: "version", Value: 1}}},
	}
	coll.Indexes().CreateMany(ctx, indexes)

	return &ScanResultModel{
		BaseModel: NewBaseModel[ScanResult](coll),
	}
}

// FindByJobID finds scan results by job ID
func (m *ScanResultModel) FindByJobID(ctx context.Context, jobID string) ([]ScanResult, error) {
	return m.FindWithSort(ctx, bson.M{"job_id": jobID}, 0, 0, "completed", -1)
}

// FindByJobIDWithPage finds scan results by job ID with pagination
func (m *ScanResultModel) FindByJobIDWithPage(ctx context.Context, jobID string, params PageParams) (*PageResult[ScanResult], error) {
	return m.FindWithPage(ctx, bson.M{"job_id": jobID}, params)
}

// FindByTargetID finds scan results by target ID
func (m *ScanResultModel) FindByTargetID(ctx context.Context, targetID string) ([]ScanResult, error) {
	return m.FindWithSort(ctx, bson.M{"target_id": targetID}, 0, 0, "completed", -1)
}

// FindByRiskLevel finds scan results by risk level
func (m *ScanResultModel) FindByRiskLevel(ctx context.Context, riskLevel string, params PageParams) (*PageResult[ScanResult], error) {
	return m.FindWithPage(ctx, bson.M{"risk_level": riskLevel}, params)
}

// FindByRiskScore finds scan results by risk score range
func (m *ScanResultModel) FindByRiskScore(ctx context.Context, minScore, maxScore float64, params PageParams) (*PageResult[ScanResult], error) {
	filter := bson.M{
		"risk_score": bson.M{
			"$gte": minScore,
			"$lte": maxScore,
		},
	}
	return m.FindWithPage(ctx, filter, params)
}

// FindHighRiskResults finds results with high risk scores
func (m *ScanResultModel) FindHighRiskResults(ctx context.Context, threshold float64, params PageParams) (*PageResult[ScanResult], error) {
	filter := bson.M{"risk_score": bson.M{"$gte": threshold}}
	// Override sort to prioritize by risk score
	params.SortBy = "risk_score"
	params.SortDesc = true
	return m.FindWithPage(ctx, filter, params)
}

// CountByJobID counts scan results by job ID
func (m *ScanResultModel) CountByJobID(ctx context.Context, jobID string) (int64, error) {
	return m.Count(ctx, bson.M{"job_id": jobID})
}

// CountByRiskLevel counts scan results by risk level
func (m *ScanResultModel) CountByRiskLevel(ctx context.Context, riskLevel string) (int64, error) {
	return m.Count(ctx, bson.M{"risk_level": riskLevel})
}

// DeleteByJobID deletes all scan results for a job
func (m *ScanResultModel) DeleteByJobID(ctx context.Context, jobID string) (int64, error) {
	return m.DeleteMany(ctx, bson.M{"job_id": jobID})
}

// AggregateRiskStats aggregates risk statistics
func (m *ScanResultModel) AggregateRiskStats(ctx context.Context) (map[string]int, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$risk_level"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := m.Coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		Level string `bson:"_id"`
		Count int    `bson:"count"`
	}
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	stats := make(map[string]int)
	for _, r := range results {
		if r.Level != "" {
			stats[r.Level] = r.Count
		} else {
			stats["unknown"] = r.Count
		}
	}
	return stats, nil
}

// AggregateAverageRiskScore calculates average risk score by job
func (m *ScanResultModel) AggregateAverageRiskScore(ctx context.Context, jobID string) (float64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "job_id", Value: jobID}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "avgRisk", Value: bson.D{{Key: "$avg", Value: "$risk_score"}}},
		}}},
	}

	cursor, err := m.Coll.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var result struct {
		AvgRisk float64 `bson:"avgRisk"`
	}
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return 0, err
		}
	}

	return result.AvgRisk, nil
}
