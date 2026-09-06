package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

// ProcessingExecutionSnapshot is an in-memory exact view of one live child.
// LeaseToken is a bearer capability and must never be persisted outside Redis.
type ProcessingExecutionSnapshot struct {
	AlreadyQuiescent   bool
	TaskInfoData       string
	TaskInfo           TaskInfo
	LeaseToken         string
	WorkerName         string
	InstanceID         string
	TaskProtocol       int
	DispatchGeneration string
	Phase              string
}

var snapshotProcessingExecutionScript = redis.NewScript(`
	local processing = redis.call('SISMEMBER', KEYS[1], ARGV[1])
	local execution = redis.call('GET', KEYS[2])
	local operation = redis.call('GET', KEYS[4])
	if processing == 0 and not execution and not operation then
		return {0, '', ''}
	end
	if operation then
		return {-2, '', ''}
	end
	if processing == 0 or not execution then
		return {-1, '', ''}
	end
	local taskInfo = redis.call('GET', KEYS[3])
	if not taskInfo then
		return {-1, '', ''}
	end
	local decoded = nil
	local payload = nil
	pcall(function() decoded = cjson.decode(execution) end)
	pcall(function() payload = cjson.decode(taskInfo) end)
	if not decoded or not payload or
		(decoded.taskId or '') ~= ARGV[1] or (payload.taskId or '') ~= ARGV[1] or
		(decoded.leaseToken or '') == '' or (decoded.workerName or '') == '' or
		(decoded.instanceId or '') == '' or tonumber(decoded.taskProtocol or 0) ~= 1 or
		(decoded.dispatchGeneration or '') == '' or
		(payload.dispatchGeneration or '') ~= (decoded.dispatchGeneration or '') then
		return {-1, '', ''}
	end
	if (decoded.phase or '') == 'pausing' then
		return {-2, '', ''}
	end
	return {1, taskInfo, execution}
`)

// SnapshotProcessingExecution atomically returns one currently processing
// execution and its immutable payload, or proves that the child is already
// quiescent. Partial/malformed ownership fails closed for recovery to resolve.
func (s *Scheduler) SnapshotProcessingExecution(ctx context.Context, taskID string) (*ProcessingExecutionSnapshot, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, ErrTaskLeaseConflict
	}
	response, err := snapshotProcessingExecutionScript.Run(ctx, s.rdb, []string{
		s.processingKey,
		taskExecutionKeyPrefix + taskID,
		taskInfoKeyPrefix + taskID,
		taskOperationGuardKeyPrefix + taskID,
	}, taskID).Result()
	if err != nil {
		return nil, err
	}
	values, ok := response.([]interface{})
	if !ok || len(values) != 3 {
		return nil, fmt.Errorf("unexpected processing execution snapshot result %T", response)
	}
	code, err := redisResultInt(values[0])
	if err != nil {
		return nil, err
	}
	switch code {
	case 0:
		return &ProcessingExecutionSnapshot{AlreadyQuiescent: true}, nil
	case -2:
		return nil, ErrTaskOperationBusy
	case 1:
	default:
		return nil, ErrTaskLeaseConflict
	}
	taskInfoData, ok := redisResultString(values[1])
	if !ok || taskInfoData == "" {
		return nil, ErrTaskLeaseConflict
	}
	executionData, ok := redisResultString(values[2])
	if !ok || executionData == "" {
		return nil, ErrTaskLeaseConflict
	}
	var taskInfo TaskInfo
	if err := json.Unmarshal([]byte(taskInfoData), &taskInfo); err != nil || taskInfo.TaskId != taskID {
		return nil, fmt.Errorf("%w: decode immutable task payload", ErrTaskLeaseConflict)
	}
	var execution struct {
		TaskID             string `json:"taskId"`
		WorkerName         string `json:"workerName"`
		InstanceID         string `json:"instanceId"`
		TaskProtocol       int    `json:"taskProtocol"`
		LeaseToken         string `json:"leaseToken"`
		DispatchGeneration string `json:"dispatchGeneration"`
		Phase              string `json:"phase"`
	}
	if err := json.Unmarshal([]byte(executionData), &execution); err != nil ||
		execution.TaskID != taskID || strings.TrimSpace(execution.LeaseToken) == "" ||
		strings.TrimSpace(execution.WorkerName) == "" || strings.TrimSpace(execution.InstanceID) == "" ||
		execution.TaskProtocol != TaskProtocolV1 || strings.TrimSpace(execution.DispatchGeneration) == "" ||
		taskInfo.DispatchGeneration != execution.DispatchGeneration {
		return nil, fmt.Errorf("%w: decode exact execution ownership", ErrTaskLeaseConflict)
	}
	return &ProcessingExecutionSnapshot{
		TaskInfoData:       taskInfoData,
		TaskInfo:           taskInfo,
		LeaseToken:         execution.LeaseToken,
		WorkerName:         execution.WorkerName,
		InstanceID:         execution.InstanceID,
		TaskProtocol:       execution.TaskProtocol,
		DispatchGeneration: execution.DispatchGeneration,
		Phase:              execution.Phase,
	}, nil
}
