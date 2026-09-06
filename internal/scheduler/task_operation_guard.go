package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	taskOperationGuardKeyPrefix = "cscan:task:operation:"
	// TaskOperationGuardTTL is deliberately much longer than the 10-15 second
	// Mongo contexts protected by the guard. TTL is crash recovery only; normal
	// completion always uses compare-delete with a unique operation token.
	TaskOperationGuardTTL = 60 * time.Second
)

// LeaseOperation is an internal capability for one bounded cross-store
// operation. OperationToken is unique per attempt, so a delayed defer cannot
// delete a later operation for the same lease.
type LeaseOperation struct {
	TaskID             string
	LeaseToken         string
	OperationToken     string
	AlreadyClosed      bool
	guardValue         string
	TaskInfoData       string
	WorkerName         string
	InstanceID         string
	TaskProtocol       int
	DispatchGeneration string
}

type leaseOperationGuardValue struct {
	LeaseToken     string `json:"leaseToken"`
	OperationToken string `json:"operationToken"`
}

// LeaseGenerationHash returns non-bearer audit evidence for an exact lease.
func LeaseGenerationHash(leaseToken string) string {
	digest := sha256.Sum256([]byte("cscan-lease-generation-v1\x00" + leaseToken))
	return hex.EncodeToString(digest[:])
}

var beginLeaseOperationScript = redis.NewScript(`
	local execution = redis.call('GET', KEYS[1])
	if not execution then
		return {-1, '', ''}
	end
	local decoded = nil
	pcall(function() decoded = cjson.decode(execution) end)
	if not decoded or (decoded.taskId or '') ~= ARGV[1] or (decoded.leaseToken or '') ~= ARGV[2] then
		return {-1, '', ''}
	end
	if tonumber(decoded.taskProtocol or 0) ~= 1 or (decoded.instanceId or '') == '' or
		(decoded.workerName or '') == '' then
		return {-1, '', ''}
	end
	if decoded.phase == 'pausing' then
		return {-2, '', ''}
	end
	if redis.call('SISMEMBER', KEYS[4], ARGV[1]) == 0 then
		return {-1, '', ''}
	end
	local taskInfo = redis.call('GET', KEYS[2])
	if not taskInfo then
		return {-1, '', ''}
	end
	local payload = nil
	pcall(function() payload = cjson.decode(taskInfo) end)
	if not payload or (payload.taskId or '') ~= ARGV[1] or
		(payload.dispatchGeneration or '') ~= (decoded.dispatchGeneration or '') then
		return {-1, '', ''}
	end
	if redis.call('EXISTS', KEYS[3]) == 1 then
		return {-2, '', ''}
	end
	local written = redis.call('SET', KEYS[3], ARGV[3], 'NX', 'PX', tonumber(ARGV[4]))
	if not written then
		return {-2, '', ''}
	end
	redis.call('EXPIRE', KEYS[1], tonumber(ARGV[5]))
	redis.call('EXPIRE', KEYS[2], tonumber(ARGV[5]))
	return {1, taskInfo, execution}
`)

// BeginLeaseOperation atomically acquires an exact-lease operation guard while
// validating processing membership and the immutable queue payload.
func (s *Scheduler) BeginLeaseOperation(ctx context.Context, taskID, leaseToken string) (*LeaseOperation, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(leaseToken) == "" {
		return nil, ErrTaskLeaseConflict
	}
	operationToken := uuid.NewString()
	guardBytes, err := json.Marshal(leaseOperationGuardValue{LeaseToken: leaseToken, OperationToken: operationToken})
	if err != nil {
		return nil, err
	}
	response, err := beginLeaseOperationScript.Run(ctx, s.rdb, []string{
		taskExecutionKeyPrefix + taskID,
		taskInfoKeyPrefix + taskID,
		taskOperationGuardKeyPrefix + taskID,
		s.processingKey,
	}, taskID, leaseToken, string(guardBytes), TaskOperationGuardTTL.Milliseconds(), int(taskMetadataTTL/time.Second)).Result()
	if err != nil {
		return nil, err
	}
	values, ok := response.([]interface{})
	if !ok || len(values) != 3 {
		return nil, fmt.Errorf("unexpected operation guard result %T", response)
	}
	code, err := redisResultInt(values[0])
	if err != nil {
		return nil, err
	}
	if code == -2 {
		return nil, ErrTaskOperationBusy
	}
	if code != 1 {
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
	var execution struct {
		WorkerName         string `json:"workerName"`
		InstanceID         string `json:"instanceId"`
		TaskProtocol       int    `json:"taskProtocol"`
		DispatchGeneration string `json:"dispatchGeneration"`
	}
	if err := json.Unmarshal([]byte(executionData), &execution); err != nil {
		return nil, fmt.Errorf("%w: decode execution evidence", ErrTaskLeaseConflict)
	}
	return &LeaseOperation{
		TaskID:             taskID,
		LeaseToken:         leaseToken,
		OperationToken:     operationToken,
		guardValue:         string(guardBytes),
		TaskInfoData:       taskInfoData,
		WorkerName:         execution.WorkerName,
		InstanceID:         execution.InstanceID,
		TaskProtocol:       execution.TaskProtocol,
		DispatchGeneration: execution.DispatchGeneration,
	}, nil
}

var releaseLeaseOperationScript = redis.NewScript(`
	if redis.call('GET', KEYS[1]) == ARGV[1] then
		return redis.call('DEL', KEYS[1])
	end
	return 0
`)

// ReleaseLeaseOperation compare-deletes only this attempt's guard.
func (s *Scheduler) ReleaseLeaseOperation(ctx context.Context, operation *LeaseOperation) error {
	if operation == nil || operation.TaskID == "" || operation.guardValue == "" {
		return nil
	}
	_, err := releaseLeaseOperationScript.Run(ctx, s.rdb,
		[]string{taskOperationGuardKeyPrefix + operation.TaskID}, operation.guardValue).Int()
	return err
}
