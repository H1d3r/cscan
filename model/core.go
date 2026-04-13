package model

import (
	"encoding/json"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ==================== Core Data Structures ====================

// ScanJob represents a scanning job in the system
type ScanJob struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TaskID   string             `bson:"task_id" json:"taskId"`
	Name     string             `bson:"name" json:"name"`
	Target   string             `bson:"target" json:"target"`
	Profile  Profile            `bson:"profile" json:"profile"`
	Tags     []string           `bson:"tags,omitempty" json:"tags,omitempty"` // 任务标签
	Status   Status             `bson:"status" json:"status"`
	Progress int                `bson:"progress" json:"progress"`
	State    TaskState          `bson:"state" json:"state"`
	Config   Config             `bson:"config" json:"config"`
	OrgID    string             `bson:"org_id,omitempty" json:"orgId,omitempty"`
	Created  time.Time          `bson:"create_time" json:"created"`
	Updated  time.Time          `bson:"update_time" json:"updated"`
	Started  *time.Time         `bson:"start_time,omitempty" json:"started,omitempty"`
	Ended    *time.Time         `bson:"end_time,omitempty" json:"ended,omitempty"`
}

// ScanTarget represents a target to be scanned
type ScanTarget struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	JobID    string             `bson:"job_id" json:"jobId"`
	Host     string             `bson:"host" json:"host"`
	Ports    []int              `bson:"ports" json:"ports"`
	Services []string           `bson:"services" json:"services"`
	Category string             `bson:"category" json:"category"`
	Priority int                `bson:"priority" json:"priority"`
	Created  time.Time          `bson:"create_time" json:"created"`
	Updated  time.Time          `bson:"update_time" json:"updated"`
}

// ScanResult represents the result of a scan operation
type ScanResult struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	JobID     string             `bson:"job_id" json:"jobId"`
	TargetID  string             `bson:"target_id" json:"targetId"`
	Authority string             `bson:"authority,omitempty" json:"authority,omitempty"` // New field for consistent association
	Host      string             `bson:"host,omitempty" json:"host,omitempty"`           // New field for consistent association
	Port      int                `bson:"port,omitempty" json:"port,omitempty"`           // New field for consistent association
	Findings  []Finding          `bson:"findings" json:"findings"`
	Assets    []Asset            `bson:"assets" json:"assets"`
	RiskScore float64            `bson:"risk_score" json:"riskScore"`
	RiskLevel string             `bson:"risk_level" json:"riskLevel"`
	Completed time.Time          `bson:"completed" json:"completed"`
	Created   time.Time          `bson:"create_time" json:"created"`
	Updated   time.Time          `bson:"update_time" json:"updated"`
	ScanTime  time.Time          `bson:"scan_time,omitempty" json:"scanTime,omitempty"` // New field for versioning
	Version   int64              `bson:"version,omitempty" json:"version,omitempty"`    // New field for versioning
}

// ==================== Supporting Types ====================

// Profile represents a scanning profile configuration
type Profile struct {
	ID          string            `bson:"id" json:"id"`
	Name        string            `bson:"name" json:"name"`
	Description string            `bson:"description" json:"description"`
	Tools       []string          `bson:"tools" json:"tools"`
	Config      map[string]string `bson:"config" json:"config"`
}

// Status represents the status of a scan job
type Status string

const (
	StatusCreated Status = "CREATED"
	StatusPending Status = "PENDING"
	StatusStarted Status = "STARTED"
	StatusPaused  Status = "PAUSED"
	StatusSuccess Status = "SUCCESS"
	StatusFailure Status = "FAILURE"
	StatusRevoked Status = "REVOKED"
	StatusStopped Status = "STOPPED"
)

// TaskState represents the execution state of a task
type TaskState struct {
	Phase       string                 `bson:"phase" json:"phase"`
	Data        map[string]interface{} `bson:"data" json:"data"`
	SubTasks    []SubTask              `bson:"sub_tasks" json:"subTasks"`
	CompletedAt *time.Time             `bson:"completed_at,omitempty" json:"completedAt,omitempty"`
}

// SubTask represents a sub-task within a main task
type SubTask struct {
	ID      string     `bson:"id" json:"id"`
	Name    string     `bson:"name" json:"name"`
	Status  Status     `bson:"status" json:"status"`
	Worker  string     `bson:"worker,omitempty" json:"worker,omitempty"`
	Result  string     `bson:"result,omitempty" json:"result,omitempty"`
	Created time.Time  `bson:"created" json:"created"`
	Started *time.Time `bson:"started,omitempty" json:"started,omitempty"`
	Ended   *time.Time `bson:"ended,omitempty" json:"ended,omitempty"`
}

// Config represents configuration data
type Config map[string]interface{}

// Finding represents a security finding
type Finding struct {
	ID          string            `bson:"id" json:"id"`
	Type        string            `bson:"type" json:"type"`
	Severity    string            `bson:"severity" json:"severity"`
	Title       string            `bson:"title" json:"title"`
	Description string            `bson:"description" json:"description"`
	Evidence    string            `bson:"evidence,omitempty" json:"evidence,omitempty"`
	References  []string          `bson:"references,omitempty" json:"references,omitempty"`
	Metadata    map[string]string `bson:"metadata,omitempty" json:"metadata,omitempty"`
	RiskScore   float64           `bson:"risk_score" json:"riskScore"`
	Discovered  time.Time         `bson:"discovered" json:"discovered"`
}

// ==================== Interface Implementations ====================

// GetId implements Identifiable interface for ScanJob
func (s *ScanJob) GetId() primitive.ObjectID {
	return s.ID
}

// SetId implements Identifiable interface for ScanJob
func (s *ScanJob) SetId(id primitive.ObjectID) {
	s.ID = id
}

// SetCreateTime implements Timestamped interface for ScanJob
func (s *ScanJob) SetCreateTime(t time.Time) {
	s.Created = t
}

// SetUpdateTime implements Timestamped interface for ScanJob
func (s *ScanJob) SetUpdateTime(t time.Time) {
	s.Updated = t
}

// GetId implements Identifiable interface for ScanTarget
func (s *ScanTarget) GetId() primitive.ObjectID {
	return s.ID
}

// SetId implements Identifiable interface for ScanTarget
func (s *ScanTarget) SetId(id primitive.ObjectID) {
	s.ID = id
}

// SetCreateTime implements Timestamped interface for ScanTarget
func (s *ScanTarget) SetCreateTime(t time.Time) {
	s.Created = t
}

// SetUpdateTime implements Timestamped interface for ScanTarget
func (s *ScanTarget) SetUpdateTime(t time.Time) {
	s.Updated = t
}

// GetId implements Identifiable interface for ScanResult
func (s *ScanResult) GetId() primitive.ObjectID {
	return s.ID
}

// SetId implements Identifiable interface for ScanResult
func (s *ScanResult) SetId(id primitive.ObjectID) {
	s.ID = id
}

// SetCreateTime implements Timestamped interface for ScanResult
func (s *ScanResult) SetCreateTime(t time.Time) {
	s.Created = t
}

// SetUpdateTime implements Timestamped interface for ScanResult
func (s *ScanResult) SetUpdateTime(t time.Time) {
	s.Updated = t
}

// ==================== Serialization Methods ====================

// MarshalJSON implements json.Marshaler for ScanJob
func (s *ScanJob) MarshalJSON() ([]byte, error) {
	type Alias ScanJob
	return json.Marshal(&struct {
		ID string `json:"id"`
		*Alias
	}{
		ID:    s.ID.Hex(),
		Alias: (*Alias)(s),
	})
}

// UnmarshalJSON implements json.Unmarshaler for ScanJob
func (s *ScanJob) UnmarshalJSON(data []byte) error {
	type Alias ScanJob
	aux := &struct {
		ID string `json:"id"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.ID != "" {
		if oid, err := primitive.ObjectIDFromHex(aux.ID); err == nil {
			s.ID = oid
		}
	}
	return nil
}

// MarshalJSON implements json.Marshaler for ScanTarget
func (s *ScanTarget) MarshalJSON() ([]byte, error) {
	type Alias ScanTarget
	return json.Marshal(&struct {
		ID string `json:"id"`
		*Alias
	}{
		ID:    s.ID.Hex(),
		Alias: (*Alias)(s),
	})
}

// UnmarshalJSON implements json.Unmarshaler for ScanTarget
func (s *ScanTarget) UnmarshalJSON(data []byte) error {
	type Alias ScanTarget
	aux := &struct {
		ID string `json:"id"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.ID != "" {
		if oid, err := primitive.ObjectIDFromHex(aux.ID); err == nil {
			s.ID = oid
		}
	}
	return nil
}

// MarshalJSON implements json.Marshaler for ScanResult
func (s *ScanResult) MarshalJSON() ([]byte, error) {
	type Alias ScanResult
	return json.Marshal(&struct {
		ID string `json:"id"`
		*Alias
	}{
		ID:    s.ID.Hex(),
		Alias: (*Alias)(s),
	})
}

// UnmarshalJSON implements json.Unmarshaler for ScanResult
func (s *ScanResult) UnmarshalJSON(data []byte) error {
	type Alias ScanResult
	aux := &struct {
		ID string `json:"id"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.ID != "" {
		if oid, err := primitive.ObjectIDFromHex(aux.ID); err == nil {
			s.ID = oid
		}
	}
	return nil
}

// ==================== Validation Methods ====================

// Validate validates a ScanJob
func (s *ScanJob) Validate() error {
	if s.Name == "" {
		return ErrValidationFailed.WithDetails("name is required")
	}
	if s.Target == "" {
		return ErrValidationFailed.WithDetails("target is required")
	}
	if s.Profile.ID == "" {
		return ErrValidationFailed.WithDetails("profile is required")
	}
	return nil
}

// Validate validates a ScanTarget
func (s *ScanTarget) Validate() error {
	if s.JobID == "" {
		return ErrValidationFailed.WithDetails("jobId is required")
	}
	if s.Host == "" {
		return ErrValidationFailed.WithDetails("host is required")
	}
	return nil
}

// Validate validates a ScanResult
func (s *ScanResult) Validate() error {
	if s.JobID == "" {
		return ErrValidationFailed.WithDetails("jobId is required")
	}
	if s.TargetID == "" {
		return ErrValidationFailed.WithDetails("targetId is required")
	}
	return nil
}
