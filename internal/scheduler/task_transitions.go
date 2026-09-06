package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var beginPausedTaskScript = redis.NewScript(`
	local execution = redis.call('GET', KEYS[1])
	if execution then
		local decoded = nil
		pcall(function() decoded = cjson.decode(execution) end)
		if not decoded or (decoded.taskId or '') ~= ARGV[1] or (decoded.leaseToken or '') ~= ARGV[2] then
			return {-1, '', ''}
		end
		if tonumber(decoded.taskProtocol or 0) ~= 1 or (decoded.instanceId or '') == '' or
			(decoded.workerName or '') == '' or (decoded.dispatchGeneration or '') == '' then
			return {-1, '', ''}
		end
		if redis.call('SISMEMBER', KEYS[5], ARGV[1]) == 0 then
			return {-1, '', ''}
		end
		local taskInfo = redis.call('GET', KEYS[3])
		if not taskInfo then
			return {-1, '', ''}
		end
		local payload = nil
		pcall(function() payload = cjson.decode(taskInfo) end)
		if not payload or (payload.taskId or '') ~= ARGV[1] or
			(payload.dispatchGeneration or '') == '' or
			(payload.dispatchGeneration or '') ~= (decoded.dispatchGeneration or '') then
			return {-1, '', ''}
		end
		if redis.call('EXISTS', KEYS[4]) == 1 then
			return {-2, '', ''}
		end
		local written = redis.call('SET', KEYS[4], ARGV[3], 'NX', 'PX', tonumber(ARGV[4]))
		if not written then
			return {-2, '', ''}
		end
		decoded.phase = 'pausing'
		decoded.lastUpdate = ARGV[5]
		decoded.lastUpdateUnix = tonumber(ARGV[6])
		redis.call('SET', KEYS[1], cjson.encode(decoded), 'EX', tonumber(ARGV[7]))
		redis.call('EXPIRE', KEYS[3], tonumber(ARGV[7]))
		return {1, taskInfo, cjson.encode(decoded)}
	end

	local status = redis.call('GET', KEYS[2])
	if status then
		local decoded = nil
		pcall(function() decoded = cjson.decode(status) end)
		if decoded and decoded.state == 'PAUSED' and (decoded.leaseToken or '') == ARGV[2] and
			(decoded.dispatchGeneration or '') ~= '' then
			local taskInfo = redis.call('GET', KEYS[3])
			local payload = nil
			if taskInfo then
				pcall(function() payload = cjson.decode(taskInfo) end)
			end
			if payload and (payload.taskId or '') == ARGV[1] and
				(payload.dispatchGeneration or '') == (decoded.dispatchGeneration or '') then
				return {0, taskInfo, status}
			end
		end
	end
	return {-1, '', ''}
`)

// BeginPausedTask atomically freezes and guards one exact lease while returning
// the immutable payload and Redis-derived owner evidence. AlreadyClosed is set
// only when retained PAUSED evidence proves this exact lease was finalized; the
// caller must still revalidate the durable parent before accepting that replay.
func (s *Scheduler) BeginPausedTask(ctx context.Context, taskID, leaseToken string) (*LeaseOperation, error) {
	if taskID == "" || leaseToken == "" {
		return nil, ErrTaskLeaseConflict
	}
	operationToken := uuid.NewString()
	guardBytes, err := json.Marshal(leaseOperationGuardValue{LeaseToken: leaseToken, OperationToken: operationToken})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	response, err := beginPausedTaskScript.Run(ctx, s.rdb, []string{
		taskExecutionKeyPrefix + taskID,
		taskStatusKeyPrefix + taskID,
		taskInfoKeyPrefix + taskID,
		taskOperationGuardKeyPrefix + taskID,
		s.processingKey,
	}, taskID, leaseToken, string(guardBytes), TaskOperationGuardTTL.Milliseconds(),
		now.Format(time.RFC3339Nano), now.Unix(), int(taskMetadataTTL/time.Second)).Result()
	if err != nil {
		return nil, err
	}
	values, ok := response.([]interface{})
	if !ok || len(values) != 3 {
		return nil, fmt.Errorf("unexpected paused operation result %T", response)
	}
	code, err := redisResultInt(values[0])
	if err != nil {
		return nil, err
	}
	if code == -2 {
		return nil, ErrTaskOperationBusy
	}
	if code != 0 && code != 1 {
		return nil, ErrTaskLeaseConflict
	}
	taskInfoData, ok := redisResultString(values[1])
	if !ok || taskInfoData == "" {
		return nil, ErrTaskLeaseConflict
	}
	evidenceData, ok := redisResultString(values[2])
	if !ok || evidenceData == "" {
		return nil, ErrTaskLeaseConflict
	}
	var evidence struct {
		WorkerName         string `json:"workerName"`
		Worker             string `json:"worker"`
		InstanceID         string `json:"instanceId"`
		TaskProtocol       int    `json:"taskProtocol"`
		DispatchGeneration string `json:"dispatchGeneration"`
	}
	if err := json.Unmarshal([]byte(evidenceData), &evidence); err != nil {
		return nil, fmt.Errorf("%w: decode paused execution evidence", ErrTaskLeaseConflict)
	}
	workerName := evidence.WorkerName
	if workerName == "" {
		workerName = evidence.Worker
	}
	if workerName == "" || evidence.InstanceID == "" || evidence.TaskProtocol != TaskProtocolV1 ||
		evidence.DispatchGeneration == "" {
		return nil, ErrTaskLeaseConflict
	}
	operation := &LeaseOperation{
		TaskID:             taskID,
		LeaseToken:         leaseToken,
		AlreadyClosed:      code == 0,
		TaskInfoData:       taskInfoData,
		WorkerName:         workerName,
		InstanceID:         evidence.InstanceID,
		TaskProtocol:       evidence.TaskProtocol,
		DispatchGeneration: evidence.DispatchGeneration,
	}
	if code == 1 {
		operation.OperationToken = operationToken
		operation.guardValue = string(guardBytes)
	}
	return operation, nil
}

var finalizePausedTaskScript = redis.NewScript(`
	if redis.call('GET', KEYS[5]) ~= ARGV[2] then
		return -2
	end
	local execution = redis.call('GET', KEYS[4])
	if not execution then
		return -1
	end
	local decoded = nil
	pcall(function() decoded = cjson.decode(execution) end)
	if not decoded or (decoded.taskId or '') ~= ARGV[3] or
		(decoded.leaseToken or '') ~= ARGV[1] or decoded.phase ~= 'pausing' or
		ARGV[6] == '' or (decoded.dispatchGeneration or '') ~= ARGV[6] then
		return -1
	end
	local taskInfo = redis.call('GET', KEYS[6])
	if not taskInfo then
		return -1
	end
	local payload = nil
	pcall(function() payload = cjson.decode(taskInfo) end)
	if not payload or (payload.taskId or '') ~= ARGV[3] or
		(payload.dispatchGeneration or '') ~= ARGV[6] then
		return -1
	end

	redis.call('SET', KEYS[1], ARGV[4], 'EX', tonumber(ARGV[7]))
	if ARGV[5] ~= '' then
		redis.call('SET', KEYS[2], ARGV[5], 'EX', tonumber(ARGV[7]))
	end
	redis.call('SREM', KEYS[3], ARGV[3])
	redis.call('DEL', KEYS[4])
	return 1
`)

// FinalizePausedTask publishes PAUSED only while the exact operation guard and
// exact pausing lease are still owned.
func (s *Scheduler) FinalizePausedTask(ctx context.Context, operation *LeaseOperation, worker, phase string) error {
	if operation == nil || operation.TaskID == "" || operation.LeaseToken == "" || operation.guardValue == "" ||
		operation.DispatchGeneration == "" || operation.WorkerName == "" || worker != operation.WorkerName {
		return ErrTaskLeaseConflict
	}
	statusData, err := json.Marshal(map[string]interface{}{
		"taskId":             operation.TaskID,
		"state":              "PAUSED",
		"worker":             worker,
		"instanceId":         operation.InstanceID,
		"taskProtocol":       operation.TaskProtocol,
		"phase":              phase,
		"leaseToken":         operation.LeaseToken,
		"dispatchGeneration": operation.DispatchGeneration,
	})
	if err != nil {
		return fmt.Errorf("marshal paused status: %w", err)
	}
	var progressData []byte
	if phase != "" {
		progressData, err = json.Marshal(map[string]interface{}{
			"currentPhase": phase, "leaseToken": operation.LeaseToken,
			"dispatchGeneration": operation.DispatchGeneration,
		})
		if err != nil {
			return fmt.Errorf("marshal paused progress: %w", err)
		}
	}
	result, err := finalizePausedTaskScript.Run(ctx, s.rdb, []string{
		taskStatusKeyPrefix + operation.TaskID,
		taskProgressKeyPrefix + operation.TaskID,
		s.processingKey,
		taskExecutionKeyPrefix + operation.TaskID,
		taskOperationGuardKeyPrefix + operation.TaskID,
		taskInfoKeyPrefix + operation.TaskID,
	}, operation.LeaseToken, operation.guardValue, operation.TaskID, string(statusData), string(progressData),
		operation.DispatchGeneration, int(taskMetadataTTL/time.Second)).Int()
	if err != nil {
		return err
	}
	if result == -2 {
		return ErrTaskOperationBusy
	}
	if result != 1 {
		return ErrTaskLeaseConflict
	}
	return nil
}

var beginStoppedTaskScript = redis.NewScript(`
	local execution = redis.call('GET', KEYS[1])
	if execution then
		local decoded = nil
		pcall(function() decoded = cjson.decode(execution) end)
		if not decoded or (decoded.taskId or '') ~= ARGV[1] or (decoded.leaseToken or '') ~= ARGV[2] then
			return {-1, '', ''}
		end
		if tonumber(decoded.taskProtocol or 0) ~= 1 or (decoded.instanceId or '') == '' or
			(decoded.workerName or '') == '' or (decoded.dispatchGeneration or '') == '' then
			return {-1, '', ''}
		end
		if redis.call('SISMEMBER', KEYS[5], ARGV[1]) == 0 then
			return {-1, '', ''}
		end
		local taskInfo = redis.call('GET', KEYS[3])
		local payload = nil
		if taskInfo then
			pcall(function() payload = cjson.decode(taskInfo) end)
		end
		if not payload or (payload.taskId or '') ~= ARGV[1] or
			(payload.dispatchGeneration or '') == '' or
			(payload.dispatchGeneration or '') ~= (decoded.dispatchGeneration or '') then
			return {-1, '', ''}
		end
		if redis.call('EXISTS', KEYS[4]) == 1 then
			return {-2, '', ''}
		end
		local written = redis.call('SET', KEYS[4], ARGV[3], 'NX', 'PX', tonumber(ARGV[4]))
		if not written then
			return {-2, '', ''}
		end
		redis.call('EXPIRE', KEYS[1], tonumber(ARGV[5]))
		redis.call('EXPIRE', KEYS[3], tonumber(ARGV[5]))
		return {1, taskInfo, execution}
	end

	local statusRaw = redis.call('GET', KEYS[2])
	local status = nil
	if statusRaw then
		pcall(function() status = cjson.decode(statusRaw) end)
	end
	if not status or (status.taskId or '') ~= ARGV[1] or
		(status.leaseToken or '') ~= ARGV[2] or (status.dispatchGeneration or '') == '' then
		return {-1, '', ''}
	end
	local taskInfo = redis.call('GET', KEYS[3])
	local payload = nil
	if taskInfo then
		pcall(function() payload = cjson.decode(taskInfo) end)
	end
	if not payload or (payload.taskId or '') ~= ARGV[1] or
		(payload.dispatchGeneration or '') ~= (status.dispatchGeneration or '') then
		return {-1, '', ''}
	end
	if (status.state or '') == 'STOPPED' then
		if redis.call('SISMEMBER', KEYS[5], ARGV[1]) == 0 then
			return {0, '', ''}
		end
		return {-1, '', ''}
	end
	if (status.state or '') ~= 'PAUSED' or tonumber(status.taskProtocol or 0) ~= 1 or
		(status.instanceId or '') == '' or (status.worker or '') == '' or
		redis.call('SISMEMBER', KEYS[5], ARGV[1]) == 1 then
		return {-1, '', ''}
	end
	if redis.call('EXISTS', KEYS[4]) == 1 then
		return {-2, '', ''}
	end
	local written = redis.call('SET', KEYS[4], ARGV[3], 'NX', 'PX', tonumber(ARGV[4]))
	if not written then
		return {-2, '', ''}
	end
	redis.call('EXPIRE', KEYS[3], tonumber(ARGV[5]))
	return {2, taskInfo, statusRaw}
`)

// BeginStoppedTask acquires a STOP-only exact-lease capability. Unlike generic
// operations, it may guard a live `pausing` execution. It can also convert an
// exact retained PAUSED closure when STOP won the durable parent race. A nil
// operation means this exact lease is already retained as STOPPED.
func (s *Scheduler) BeginStoppedTask(ctx context.Context, taskID, leaseToken string) (*LeaseOperation, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(leaseToken) == "" {
		return nil, ErrTaskLeaseConflict
	}
	operationToken := uuid.NewString()
	guardBytes, err := json.Marshal(leaseOperationGuardValue{LeaseToken: leaseToken, OperationToken: operationToken})
	if err != nil {
		return nil, err
	}
	response, err := beginStoppedTaskScript.Run(ctx, s.rdb, []string{
		taskExecutionKeyPrefix + taskID,
		taskStatusKeyPrefix + taskID,
		taskInfoKeyPrefix + taskID,
		taskOperationGuardKeyPrefix + taskID,
		s.processingKey,
	}, taskID, leaseToken, string(guardBytes), TaskOperationGuardTTL.Milliseconds(),
		int(taskMetadataTTL/time.Second)).Result()
	if err != nil {
		return nil, err
	}
	values, ok := response.([]interface{})
	if !ok || len(values) != 3 {
		return nil, fmt.Errorf("unexpected stopped operation result %T", response)
	}
	code, err := redisResultInt(values[0])
	if err != nil {
		return nil, err
	}
	if code == 0 {
		return nil, nil
	}
	if code == -2 {
		return nil, ErrTaskOperationBusy
	}
	if code != 1 && code != 2 {
		return nil, ErrTaskLeaseConflict
	}
	taskInfoData, ok := redisResultString(values[1])
	if !ok || taskInfoData == "" {
		return nil, ErrTaskLeaseConflict
	}
	evidenceData, ok := redisResultString(values[2])
	if !ok || evidenceData == "" {
		return nil, ErrTaskLeaseConflict
	}
	var evidence struct {
		WorkerName         string `json:"workerName"`
		Worker             string `json:"worker"`
		InstanceID         string `json:"instanceId"`
		TaskProtocol       int    `json:"taskProtocol"`
		DispatchGeneration string `json:"dispatchGeneration"`
	}
	if err := json.Unmarshal([]byte(evidenceData), &evidence); err != nil {
		return nil, fmt.Errorf("%w: decode stopped execution evidence", ErrTaskLeaseConflict)
	}
	workerName := evidence.WorkerName
	if workerName == "" {
		workerName = evidence.Worker
	}
	if workerName == "" || evidence.InstanceID == "" || evidence.TaskProtocol != TaskProtocolV1 ||
		evidence.DispatchGeneration == "" {
		return nil, ErrTaskLeaseConflict
	}
	return &LeaseOperation{
		TaskID:             taskID,
		LeaseToken:         leaseToken,
		OperationToken:     operationToken,
		guardValue:         string(guardBytes),
		TaskInfoData:       taskInfoData,
		WorkerName:         workerName,
		InstanceID:         evidence.InstanceID,
		TaskProtocol:       evidence.TaskProtocol,
		DispatchGeneration: evidence.DispatchGeneration,
	}, nil
}

var finalizeStoppedTaskScript = redis.NewScript(`
	if redis.call('GET', KEYS[5]) ~= ARGV[2] then
		return -2
	end
	local taskInfo = redis.call('GET', KEYS[6])
	local payload = nil
	if taskInfo then
		pcall(function() payload = cjson.decode(taskInfo) end)
	end
	if not payload or (payload.taskId or '') ~= ARGV[3] or
		(payload.dispatchGeneration or '') ~= ARGV[6] then
		return -1
	end

	local executionRaw = redis.call('GET', KEYS[4])
	if executionRaw then
		local execution = nil
		pcall(function() execution = cjson.decode(executionRaw) end)
		if not execution or (execution.taskId or '') ~= ARGV[3] or
			(execution.leaseToken or '') ~= ARGV[1] or
			(execution.dispatchGeneration or '') ~= ARGV[6] or
			(execution.workerName or '') ~= ARGV[9] or
			(execution.instanceId or '') ~= ARGV[10] or
			tonumber(execution.taskProtocol or 0) ~= tonumber(ARGV[11]) or
			redis.call('SISMEMBER', KEYS[3], ARGV[3]) == 0 then
			return -1
		end
	else
		local statusRaw = redis.call('GET', KEYS[1])
		local status = nil
		if statusRaw then
			pcall(function() status = cjson.decode(statusRaw) end)
		end
		if not status or (status.taskId or '') ~= ARGV[3] or
			(status.leaseToken or '') ~= ARGV[1] or (status.state or '') ~= 'PAUSED' or
			(status.dispatchGeneration or '') ~= ARGV[6] or (status.worker or '') ~= ARGV[9] or
			(status.instanceId or '') ~= ARGV[10] or
			tonumber(status.taskProtocol or 0) ~= tonumber(ARGV[11]) or
			redis.call('SISMEMBER', KEYS[3], ARGV[3]) == 1 then
			return -1
		end
	end

	redis.call('SET', KEYS[1], ARGV[4], 'EX', tonumber(ARGV[7]))
	if ARGV[5] ~= '' then
		redis.call('SET', KEYS[2], ARGV[5], 'EX', tonumber(ARGV[7]))
	end
	redis.call('SREM', KEYS[3], ARGV[3])
	redis.call('DEL', KEYS[4])
	redis.call('EXPIRE', KEYS[6], tonumber(ARGV[7]))
	redis.call('SADD', KEYS[7], ARGV[8])
	return 1
`)

// FinalizeStoppedTask atomically closes a live running/pausing exact lease, or
// converts its exact retained PAUSED closure, while the STOP-only guard remains
// owned. Generic progress and terminal operations still reject `pausing`.
func (s *Scheduler) FinalizeStoppedTask(
	ctx context.Context,
	operation *LeaseOperation,
	worker, resultText, phase string,
) error {
	if operation == nil || operation.TaskID == "" || operation.LeaseToken == "" || operation.guardValue == "" ||
		operation.DispatchGeneration == "" || operation.WorkerName == "" || operation.InstanceID == "" ||
		operation.TaskProtocol != TaskProtocolV1 || worker != operation.WorkerName {
		return ErrTaskLeaseConflict
	}
	statusData, err := json.Marshal(map[string]interface{}{
		"taskId":             operation.TaskID,
		"state":              TaskStatusStopped,
		"worker":             worker,
		"instanceId":         operation.InstanceID,
		"taskProtocol":       operation.TaskProtocol,
		"result":             resultText,
		"phase":              phase,
		"leaseToken":         operation.LeaseToken,
		"dispatchGeneration": operation.DispatchGeneration,
	})
	if err != nil {
		return fmt.Errorf("marshal stopped status: %w", err)
	}
	progressData := ""
	if phase != "" {
		data, marshalErr := json.Marshal(map[string]interface{}{
			"currentPhase":       phase,
			"leaseToken":         operation.LeaseToken,
			"dispatchGeneration": operation.DispatchGeneration,
		})
		if marshalErr != nil {
			return fmt.Errorf("marshal stopped progress: %w", marshalErr)
		}
		progressData = string(data)
	}
	completedData, _ := json.Marshal(TaskInfo{
		TaskId: operation.TaskID, DispatchGeneration: operation.DispatchGeneration,
	})
	result, err := finalizeStoppedTaskScript.Run(ctx, s.rdb, []string{
		taskStatusKeyPrefix + operation.TaskID,
		taskProgressKeyPrefix + operation.TaskID,
		s.processingKey,
		taskExecutionKeyPrefix + operation.TaskID,
		taskOperationGuardKeyPrefix + operation.TaskID,
		taskInfoKeyPrefix + operation.TaskID,
		taskCompletedSet,
	}, operation.LeaseToken, operation.guardValue, operation.TaskID, string(statusData), progressData,
		operation.DispatchGeneration, int(taskMetadataTTL/time.Second), string(completedData),
		operation.WorkerName, operation.InstanceID, operation.TaskProtocol).Int()
	if err != nil {
		return err
	}
	if result == -2 {
		return ErrTaskOperationBusy
	}
	if result != 1 {
		return ErrTaskLeaseConflict
	}
	return nil
}

var updateLeasedTaskScript = redis.NewScript(`
	local terminal = ARGV[6] == '1'
	local expectedDispatch = ARGV[14]
	local execution = redis.call('GET', KEYS[4])
	if execution then
		local decoded = nil
		pcall(function() decoded = cjson.decode(execution) end)
		if not decoded or (decoded.taskId or '') ~= ARGV[1] or (decoded.leaseToken or '') ~= ARGV[2] then
			return {-1, ''}
		end
		if decoded.phase == 'pausing' then
			return {-1, ''}
		end
	elseif ARGV[2] ~= '' then
		-- An execution-less replay is acknowledgement-only and is valid solely
		-- for the exact terminal state already committed by this lease.
		if not terminal then
			return {-1, ''}
		end
		local status = redis.call('GET', KEYS[1])
		local taskInfoData = redis.call('GET', KEYS[5])
		if status and taskInfoData then
			local decoded = nil
			local payload = nil
			pcall(function() decoded = cjson.decode(status) end)
			pcall(function() payload = cjson.decode(taskInfoData) end)
			if decoded and payload and (decoded.taskId or '') == ARGV[1] and
				(payload.taskId or '') == ARGV[1] and (decoded.leaseToken or '') == ARGV[2] and
				(decoded.state or '') == ARGV[3] and
				(payload.dispatchGeneration or '') == (decoded.dispatchGeneration or '') and
				(expectedDispatch == '' or (decoded.dispatchGeneration or '') == expectedDispatch) then
				return {0, taskInfoData}
			end
		end
		return {-1, ''}
	end

	local taskInfo = redis.call('GET', KEYS[5]) or ''
	local metadataTTL = tonumber(ARGV[12])
	local expectedOperation = ARGV[13]
	local currentOperation = redis.call('GET', KEYS[7])
	if expectedOperation ~= '' then
		if currentOperation ~= expectedOperation then
			return {-2, ''}
		end
	elseif terminal and currentOperation then
		return {-2, ''}
	end
	if execution then
		local payload = nil
		if taskInfo ~= '' then
			pcall(function() payload = cjson.decode(taskInfo) end)
		end
		local decoded = cjson.decode(execution)
		if not payload or (payload.taskId or '') ~= ARGV[1] or
			(payload.dispatchGeneration or '') ~= (decoded.dispatchGeneration or '') or
			(expectedDispatch ~= '' and (decoded.dispatchGeneration or '') ~= expectedDispatch) then
			return {-1, ''}
		end
	end
	-- A live tokenized execution is recoverable only while its exact queue
	-- payload exists. Fail closed before writing status/progress if that
	-- payload has expired.
	if execution and not terminal and taskInfo == '' then
		return {-1, ''}
	end

	redis.call('SET', KEYS[1], ARGV[4], 'EX', metadataTTL)
	if ARGV[5] ~= '' then
		redis.call('SET', KEYS[2], ARGV[5], 'EX', metadataTTL)
	end

	if terminal then
		redis.call('SREM', KEYS[3], ARGV[1])
		redis.call('DEL', KEYS[4])
		redis.call('EXPIRE', KEYS[5], metadataTTL)
		redis.call('SADD', KEYS[6], ARGV[7])
	else
		if execution then
			local decoded = cjson.decode(execution)
			decoded.lastUpdate = ARGV[8]
			decoded.lastUpdateUnix = tonumber(ARGV[11])
			if ARGV[9] ~= '' then
				decoded.phase = ARGV[9]
			end
			decoded.progress = tonumber(ARGV[10])
			redis.call('SET', KEYS[4], cjson.encode(decoded), 'EX', metadataTTL)
			redis.call('EXPIRE', KEYS[5], metadataTTL)
		end
	end
	return {1, taskInfo}
`)

// updateLeasedTask writes progress/status and, for terminal states, atomically
// releases ownership. When operation is non-nil, the same exact guard that
// protected the caller's bounded Mongo write must still be present.
func (s *Scheduler) updateLeasedTask(
	ctx context.Context,
	operation *LeaseOperation,
	taskID, leaseToken, worker, state, resultText, phase string,
	progress int,
) (string, error) {
	if taskID == "" {
		return "", fmt.Errorf("task id cannot be empty")
	}
	guardValue := ""
	dispatchGeneration := ""
	if operation != nil {
		if operation.TaskID != taskID || operation.LeaseToken != leaseToken || operation.guardValue == "" ||
			(worker != "" && worker != operation.WorkerName) {
			return "", ErrTaskLeaseConflict
		}
		guardValue = operation.guardValue
		dispatchGeneration = operation.DispatchGeneration
		worker = operation.WorkerName
	}
	statusData, err := json.Marshal(map[string]interface{}{
		"taskId":             taskID,
		"state":              state,
		"worker":             worker,
		"result":             resultText,
		"phase":              phase,
		"leaseToken":         leaseToken,
		"dispatchGeneration": dispatchGeneration,
	})
	if err != nil {
		return "", err
	}
	progressData := ""
	if phase != "" {
		data, err := json.Marshal(map[string]interface{}{
			"currentPhase":       phase,
			"leaseToken":         leaseToken,
			"dispatchGeneration": dispatchGeneration,
		})
		if err != nil {
			return "", err
		}
		progressData = string(data)
	}
	terminal := isTerminalTaskState(state)
	terminalArg := "0"
	if terminal {
		terminalArg = "1"
	}
	completedData, _ := json.Marshal(TaskInfo{TaskId: taskID, DispatchGeneration: dispatchGeneration})
	now := time.Now().UTC()
	response, err := updateLeasedTaskScript.Run(ctx, s.rdb, []string{
		taskStatusKeyPrefix + taskID,
		taskProgressKeyPrefix + taskID,
		s.processingKey,
		taskExecutionKeyPrefix + taskID,
		taskInfoKeyPrefix + taskID,
		taskCompletedSet,
		taskOperationGuardKeyPrefix + taskID,
	}, taskID, leaseToken, state, string(statusData), progressData, terminalArg, string(completedData),
		now.Format(time.RFC3339Nano), phase, progress, now.Unix(), int(taskMetadataTTL/time.Second),
		guardValue, dispatchGeneration).Result()
	if err != nil {
		return "", err
	}
	values, ok := response.([]interface{})
	if !ok || len(values) != 2 {
		return "", fmt.Errorf("unexpected leased update result %T", response)
	}
	code, err := redisResultInt(values[0])
	if err != nil {
		return "", err
	}
	if code == -2 {
		return "", ErrTaskOperationBusy
	}
	if code < 0 {
		return "", ErrTaskLeaseConflict
	}
	taskInfo, _ := redisResultString(values[1])
	return taskInfo, nil
}

// UpdateLeasedTask is retained for Redis-only compatibility paths. Any caller
// that crosses into Mongo must use UpdateLeasedTaskWithOperation.
func (s *Scheduler) UpdateLeasedTask(
	ctx context.Context,
	taskID, leaseToken, worker, state, resultText, phase string,
	progress int,
) (string, error) {
	return s.updateLeasedTask(ctx, nil, taskID, leaseToken, worker, state, resultText, phase, progress)
}

func isTerminalTaskState(state string) bool {
	return state == TaskStatusSuccess || state == TaskStatusPartial || state == TaskStatusFailure ||
		state == TaskStatusStopped || state == TaskStatusRevoked || state == "COMPLETED"
}

func (s *Scheduler) UpdateLeasedTaskWithOperation(
	ctx context.Context,
	operation *LeaseOperation,
	worker, state, resultText, phase string,
	progress int,
) (string, error) {
	if operation == nil {
		return "", ErrTaskLeaseConflict
	}
	return s.updateLeasedTask(ctx, operation, operation.TaskID, operation.LeaseToken,
		worker, state, resultText, phase, progress)
}

var confirmClosedTaskLeaseScript = redis.NewScript(`
	if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 1 or redis.call('EXISTS', KEYS[2]) == 1 then
		return {-1, ''}
	end
	local statusData = redis.call('GET', KEYS[3])
	if not statusData then
		return {-1, ''}
	end
	local status = nil
	pcall(function() status = cjson.decode(statusData) end)
	if not status or (status.taskId or '') ~= ARGV[1] or
		(status.leaseToken or '') ~= ARGV[2] or (status.state or '') ~= ARGV[3] then
		return {-1, ''}
	end
	local taskInfoData = redis.call('GET', KEYS[4])
	if not taskInfoData then
		return {-1, ''}
	end
	local taskInfo = nil
	pcall(function() taskInfo = cjson.decode(taskInfoData) end)
	if not taskInfo or (taskInfo.taskId or '') ~= ARGV[1] or
		(taskInfo.dispatchGeneration or '') ~= (status.dispatchGeneration or '') then
		return {-1, ''}
	end
	return {1, taskInfoData}
`)

// ConfirmClosedTaskLease recognizes an ambiguous terminal response only when
// the exact lease has already released processing ownership and its retained
// status and immutable payload still agree on the dispatch generation. It is
// read-only and never turns an ownership mismatch into success.
func (s *Scheduler) ConfirmClosedTaskLease(ctx context.Context, taskID, leaseToken, state string) (*TaskInfo, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(leaseToken) == "" || !isTerminalTaskState(state) {
		return nil, ErrTaskLeaseConflict
	}
	response, err := confirmClosedTaskLeaseScript.Run(ctx, s.rdb, []string{
		s.processingKey,
		taskExecutionKeyPrefix + taskID,
		taskStatusKeyPrefix + taskID,
		taskInfoKeyPrefix + taskID,
	}, taskID, leaseToken, state).Result()
	if err != nil {
		return nil, err
	}
	values, ok := response.([]interface{})
	if !ok || len(values) != 2 {
		return nil, fmt.Errorf("unexpected closed lease confirmation result %T", response)
	}
	code, err := redisResultInt(values[0])
	if err != nil {
		return nil, err
	}
	if code != 1 {
		return nil, ErrTaskLeaseConflict
	}
	taskInfoData, ok := redisResultString(values[1])
	if !ok || taskInfoData == "" {
		return nil, ErrTaskLeaseConflict
	}
	var taskInfo TaskInfo
	if err := json.Unmarshal([]byte(taskInfoData), &taskInfo); err != nil || taskInfo.TaskId != taskID {
		return nil, ErrTaskLeaseConflict
	}
	return &taskInfo, nil
}

var renewTaskLeaseScript = redis.NewScript(`
	local execution = redis.call('GET', KEYS[1])
	if not execution then
		return {-1, ''}
	end
	local decoded = nil
	pcall(function() decoded = cjson.decode(execution) end)
	if not decoded or (decoded.taskId or '') ~= ARGV[1] or (decoded.leaseToken or '') ~= ARGV[2] then
		return {-1, ''}
	end
	if decoded.phase == 'pausing' then
		return {-1, ''}
	end
	local taskInfo = redis.call('GET', KEYS[2])
	if not taskInfo then
		return {-1, ''}
	end
	local metadataTTL = tonumber(ARGV[5])
	decoded.lastUpdate = ARGV[3]
	decoded.lastUpdateUnix = tonumber(ARGV[4])
	redis.call('SET', KEYS[1], cjson.encode(decoded), 'EX', metadataTTL)
	redis.call('EXPIRE', KEYS[2], metadataTTL)
	return {1, taskInfo}
`)

// FenceTaskLease validates and refreshes one exact execution generation and
// returns its immutable queue payload from the same atomic operation. Callers
// must validate the payload before performing owner-sensitive side effects.
func (s *Scheduler) FenceTaskLease(ctx context.Context, taskID, leaseToken string) (string, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(leaseToken) == "" {
		return "", fmt.Errorf("task lease renewal requires task id and lease token")
	}
	now := time.Now().UTC()
	response, err := renewTaskLeaseScript.Run(ctx, s.rdb, []string{
		taskExecutionKeyPrefix + taskID,
		taskInfoKeyPrefix + taskID,
	}, taskID, leaseToken, now.Format(time.RFC3339Nano), now.Unix(), int(taskMetadataTTL/time.Second)).Result()
	if err != nil {
		return "", err
	}
	values, ok := response.([]interface{})
	if !ok || len(values) != 2 {
		return "", fmt.Errorf("unexpected lease fence result %T", response)
	}
	code, err := redisResultInt(values[0])
	if err != nil {
		return "", err
	}
	if code != 1 {
		return "", ErrTaskLeaseConflict
	}
	taskInfo, ok := redisResultString(values[1])
	if !ok || taskInfo == "" {
		return "", ErrTaskLeaseConflict
	}
	return taskInfo, nil
}

// RenewTaskLease refreshes one exact execution generation without changing
// its phase, progress, or externally visible task status.
func (s *Scheduler) RenewTaskLease(ctx context.Context, taskID, leaseToken string) error {
	_, err := s.FenceTaskLease(ctx, taskID, leaseToken)
	return err
}

func redisResultInt(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		var result int64
		_, err := fmt.Sscan(typed, &result)
		return result, err
	default:
		return 0, fmt.Errorf("unexpected Redis integer type %T", value)
	}
}

// ResumeTaskBatch atomically verifies that every paused child has released its
// lease, removes every canonical queued location, publishes one new queue
// generation, and clears only byte-exact old-generation PAUSE envelopes. The
// marker makes ambiguous retries idempotent.
func (s *Scheduler) ResumeTaskBatch(ctx context.Context, tasks []*TaskInfo, pauseControls []TaskControlEnvelope, markerKey string) error {
	if len(tasks) == 0 {
		return fmt.Errorf("resume task batch cannot be empty")
	}
	if markerKey == "" {
		return fmt.Errorf("resume marker key cannot be empty")
	}
	childIDs := make(map[string]struct{}, len(tasks))
	mainTaskID := ""
	newGeneration := ""
	for _, task := range tasks {
		if task == nil || task.TaskId == "" || task.MainTaskId == "" || task.Config == "" || task.DispatchGeneration == "" {
			return fmt.Errorf("resume task requires task id, main task id, config, and generation")
		}
		if mainTaskID == "" {
			mainTaskID = task.MainTaskId
			newGeneration = task.DispatchGeneration
		}
		if task.MainTaskId != mainTaskID || task.DispatchGeneration != newGeneration {
			return fmt.Errorf("resume task batch must share one parent and generation")
		}
		childIDs[task.TaskId] = struct{}{}
	}
	type expectedControl struct {
		key  string
		data []byte
	}
	expectedControls := make([]expectedControl, 0, len(pauseControls))
	seenControls := make(map[string]struct{}, len(pauseControls))
	for _, envelope := range pauseControls {
		if err := envelope.Validate(); err != nil {
			return err
		}
		if envelope.Action != TaskControlActionPause || envelope.MainTaskID != mainTaskID ||
			envelope.DispatchGeneration == newGeneration {
			return fmt.Errorf("resume cleanup requires exact old-generation PAUSE envelopes")
		}
		if _, ok := childIDs[envelope.TaskID]; !ok {
			return fmt.Errorf("resume cleanup task %s is outside the canonical batch", envelope.TaskID)
		}
		key, _ := envelope.Key()
		if _, duplicate := seenControls[key]; duplicate {
			return fmt.Errorf("resume cleanup control %s is duplicated", key)
		}
		seenControls[key] = struct{}{}
		data, err := marshalTaskControlEnvelope(envelope)
		if err != nil {
			return err
		}
		expectedControls = append(expectedControls, expectedControl{key: key, data: data})
	}
	entries, taskIDs, err := s.buildQueuedTaskEntries(tasks, false)
	if err != nil {
		return err
	}

	args := make([]interface{}, 0, 11+len(taskIDs)+4*len(entries)+2*len(expectedControls))
	args = append(args,
		taskQueuedKeyPrefix,
		taskExecutionKeyPrefix,
		taskInfoKeyPrefix,
		taskStatusKeyPrefix,
		taskProgressKeyPrefix,
		len(taskIDs),
		len(entries),
		len(expectedControls),
	)
	for _, taskID := range taskIDs {
		args = append(args, taskID)
	}
	for _, entry := range entries {
		args = append(args, entry.taskID, entry.queue, entry.score, entry.member)
	}
	for _, control := range expectedControls {
		args = append(args, control.key, control.data)
	}
	args = append(args, taskOperationGuardKeyPrefix, publicationMarkerValue(markerKey, entries), int(taskMetadataTTL/time.Second))

	script := redis.NewScript(`
		local operationPrefix = ARGV[#ARGV - 2]
		local markerValue = ARGV[#ARGV - 1]
		local metadataTTL = tonumber(ARGV[#ARGV])
		local existingMarker = redis.call('GET', KEYS[1])
		if existingMarker then
			if existingMarker == markerValue then
				return {0, ''}
			end
			return {-3, ''}
		end
		local queuedPrefix = ARGV[1]
		local executionPrefix = ARGV[2]
		local infoPrefix = ARGV[3]
		local statusPrefix = ARGV[4]
		local progressPrefix = ARGV[5]
		local taskCount = tonumber(ARGV[6])
		local entryCount = tonumber(ARGV[7])
		local controlCount = tonumber(ARGV[8])
		local selected = {}

		for index = 1, taskCount do
			local taskID = ARGV[8 + index]
			selected[taskID] = true
			if redis.call('EXISTS', operationPrefix .. taskID) == 1 then
				return {-2, taskID}
			end
			if redis.call('SISMEMBER', KEYS[2], taskID) == 1 or redis.call('EXISTS', executionPrefix .. taskID) == 1 then
				return {-1, taskID}
			end
		end

		for index = 1, taskCount do
			local taskID = ARGV[8 + index]
			local locationKey = queuedPrefix .. taskID
			local locations = redis.call('HGETALL', locationKey)
			for locationIndex = 1, #locations, 2 do
				redis.call('ZREM', locations[locationIndex], locations[locationIndex + 1])
			end
			redis.call('DEL', locationKey)
		end

		local queues = redis.call('SMEMBERS', KEYS[3])
		for _, queueKey in ipairs(queues) do
			local members = redis.call('ZRANGE', queueKey, 0, -1)
			for _, member in ipairs(members) do
				local decoded = nil
				pcall(function() decoded = cjson.decode(member) end)
				if decoded and decoded.taskId and selected[decoded.taskId] then
					redis.call('ZREM', queueKey, member)
				end
			end
		end

		local entryBase = 9 + taskCount
		for index = 1, entryCount do
			local offset = entryBase + ((index - 1) * 4)
			local taskID = ARGV[offset]
			local queueKey = ARGV[offset + 1]
			local score = ARGV[offset + 2]
			local member = ARGV[offset + 3]
			redis.call('ZADD', queueKey, score, member)
			redis.call('HSET', queuedPrefix .. taskID, queueKey, member)
			redis.call('SADD', KEYS[3], queueKey)
			redis.call('SET', infoPrefix .. taskID, member, 'EX', metadataTTL)
		end

		for index = 1, taskCount do
			local taskID = ARGV[8 + index]
			redis.call('DEL', statusPrefix .. taskID)
			redis.call('DEL', progressPrefix .. taskID)
		end
		local controlBase = entryBase + (entryCount * 4)
		for index = 1, controlCount do
			local offset = controlBase + ((index - 1) * 2)
			local controlKey = ARGV[offset]
			local expectedEnvelope = ARGV[offset + 1]
			if redis.call('GET', controlKey) == expectedEnvelope then
				redis.call('DEL', controlKey)
			end
		end
		redis.call('SET', KEYS[1], markerValue, 'EX', metadataTTL)
		return {entryCount, ''}
	`)
	response, err := script.Run(ctx, s.rdb, []string{markerKey, s.processingKey, taskQueueKeysSet}, args...).Result()
	if err != nil {
		return err
	}
	values, ok := response.([]interface{})
	if !ok || len(values) != 2 {
		return fmt.Errorf("unexpected resume result %T", response)
	}
	code, err := redisResultInt(values[0])
	if err != nil {
		return err
	}
	switch code {
	case -1:
		taskID, _ := redisResultString(values[1])
		return fmt.Errorf("%w: %s", ErrTaskAlreadyProcessing, taskID)
	case -2:
		return ErrTaskOperationBusy
	case -3:
		return fmt.Errorf("resume marker manifest mismatch")
	}
	if code > 0 {
		s.notifyTaskAvailable(ctx)
	}
	return nil
}

func recoveryMarker(tasks []*TaskInfo) string {
	parts := make([]string, 0, len(tasks))
	for _, task := range tasks {
		parts = append(parts, task.TaskId+"\x00"+task.DispatchGeneration+"\x00"+task.LeaseToken)
	}
	sort.Strings(parts)
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return "cscan:task:requeue:" + hex.EncodeToString(digest[:])
}

func (s *Scheduler) prepareRecoveredTask(ctx context.Context, task *TaskInfo) (*TaskInfo, error) {
	if task == nil || task.TaskId == "" || task.MainTaskId == "" || task.Config == "" || task.LeaseToken == "" ||
		(hasMongoParentID(task.MainTaskId) && task.DispatchGeneration == "") {
		return nil, fmt.Errorf("exact recovery requires payload identity, active dispatch generation, config, and lease token")
	}
	taskCopy := *task
	if len(taskCopy.Workers) > 0 {
		selectedWorker := ""
		for _, workerName := range taskCopy.Workers {
			exists, err := s.rdb.Exists(ctx, "cscan:worker:"+workerName).Result()
			if err != nil {
				return nil, fmt.Errorf("check worker %s heartbeat: %w", workerName, err)
			}
			if exists > 0 {
				selectedWorker = workerName
				break
			}
		}
		if selectedWorker != "" {
			taskCopy.Workers = []string{selectedWorker}
		} else {
			taskCopy.Workers = nil
		}
	}
	if strings.TrimSpace(taskCopy.CreateTime) == "" {
		taskCopy.CreateTime = time.Now().Local().Format("2006-01-02 15:04:05")
	}
	return &taskCopy, nil
}

// RequeueExactTaskBatch transfers a complete sibling recovery plan in one Lua
// commit. Every observed lease is revalidated before any queue or ownership
// mutation, and pausing leases are never stolen.
func (s *Scheduler) RequeueExactTaskBatch(ctx context.Context, tasks []*TaskInfo) (bool, error) {
	if len(tasks) == 0 {
		return false, fmt.Errorf("exact recovery batch cannot be empty")
	}
	prepared := make([]*TaskInfo, 0, len(tasks))
	for _, task := range tasks {
		preparedTask, err := s.prepareRecoveredTask(ctx, task)
		if err != nil {
			return false, err
		}
		prepared = append(prepared, preparedTask)
	}
	entries, taskIDs, err := s.buildQueuedTaskEntries(prepared, false)
	if err != nil {
		return false, err
	}
	if len(entries) != len(taskIDs) || len(taskIDs) != len(prepared) {
		return false, fmt.Errorf("recovery plan must have exactly one destination per task")
	}
	byID := make(map[string]*TaskInfo, len(prepared))
	for _, task := range prepared {
		if _, duplicate := byID[task.TaskId]; duplicate {
			return false, fmt.Errorf("duplicate recovery task id %s", task.TaskId)
		}
		byID[task.TaskId] = task
	}

	markerKey := recoveryMarker(prepared)
	args := make([]interface{}, 0, 10+4*len(taskIDs)+4*len(entries))
	args = append(args, taskQueuedKeyPrefix, taskExecutionKeyPrefix, taskInfoKeyPrefix, taskStatusKeyPrefix, taskProgressKeyPrefix, len(taskIDs))
	for _, taskID := range taskIDs {
		args = append(args, taskID, byID[taskID].LeaseToken, byID[taskID].RecoveryInstanceID, byID[taskID].DispatchGeneration)
	}
	for _, entry := range entries {
		args = append(args, entry.taskID, entry.queue, entry.score, entry.member)
	}
	args = append(args, taskOperationGuardKeyPrefix, "cscan:worker:instance:", publicationMarkerValue(markerKey, entries), int(taskMetadataTTL/time.Second))

	script := redis.NewScript(`
		local operationPrefix = ARGV[#ARGV - 3]
		local heartbeatPrefix = ARGV[#ARGV - 2]
		local markerValue = ARGV[#ARGV - 1]
		local metadataTTL = tonumber(ARGV[#ARGV])
		local existingMarker = redis.call('GET', KEYS[1])
		if existingMarker then
			if existingMarker == markerValue then
				return {0, ''}
			end
			return {-3, ''}
		end
		local queuedPrefix = ARGV[1]
		local executionPrefix = ARGV[2]
		local infoPrefix = ARGV[3]
		local statusPrefix = ARGV[4]
		local progressPrefix = ARGV[5]
		local taskCount = tonumber(ARGV[6])

		for index = 1, taskCount do
			local offset = 7 + ((index - 1) * 4)
			local taskID = ARGV[offset]
			local expectedLease = ARGV[offset + 1]
			local expectedInstance = ARGV[offset + 2]
			local expectedDispatch = ARGV[offset + 3]
			if redis.call('EXISTS', operationPrefix .. taskID) == 1 then
				return {-2, taskID}
			end
			if redis.call('SISMEMBER', KEYS[2], taskID) == 0 then
				return {-1, taskID}
			end
			local execution = redis.call('GET', executionPrefix .. taskID)
			if not execution then
				return {-1, taskID}
			end
			local decoded = nil
			pcall(function() decoded = cjson.decode(execution) end)
			if not decoded or (decoded.taskId or '') ~= taskID or
				(decoded.leaseToken or '') ~= expectedLease or decoded.phase == 'pausing' or
				(decoded.dispatchGeneration or '') ~= expectedDispatch then
				return {-1, taskID}
			end
			local taskInfo = redis.call('GET', infoPrefix .. taskID)
			local payload = nil
			if taskInfo then
				pcall(function() payload = cjson.decode(taskInfo) end)
			end
			if not payload or (payload.taskId or '') ~= taskID or
				(payload.dispatchGeneration or '') ~= expectedDispatch then
				return {-1, taskID}
			end
			if expectedInstance ~= '' then
				if (decoded.instanceId or '') ~= expectedInstance or tonumber(decoded.taskProtocol or 0) ~= 1 then
					return {-1, taskID}
				end
				if redis.call('EXISTS', heartbeatPrefix .. expectedInstance) == 1 then
					return {-1, taskID}
				end
			end
		end

		for index = 1, taskCount do
			local offset = 7 + ((index - 1) * 4)
			local taskID = ARGV[offset]
			local locationKey = queuedPrefix .. taskID
			local locations = redis.call('HGETALL', locationKey)
			for locationIndex = 1, #locations, 2 do
				redis.call('ZREM', locations[locationIndex], locations[locationIndex + 1])
			end
			redis.call('DEL', locationKey)
		end

		local entryBase = 7 + (taskCount * 4)
		for index = 1, taskCount do
			local offset = entryBase + ((index - 1) * 4)
			local taskID = ARGV[offset]
			local queueKey = ARGV[offset + 1]
			local score = ARGV[offset + 2]
			local member = ARGV[offset + 3]
			redis.call('ZADD', queueKey, score, member)
			redis.call('HSET', queuedPrefix .. taskID, queueKey, member)
			redis.call('SADD', KEYS[3], queueKey)
			redis.call('SET', infoPrefix .. taskID, member, 'EX', metadataTTL)
			redis.call('SREM', KEYS[2], taskID)
			redis.call('DEL', executionPrefix .. taskID)
			redis.call('DEL', statusPrefix .. taskID)
			redis.call('DEL', progressPrefix .. taskID)
		end
		redis.call('SET', KEYS[1], markerValue, 'EX', metadataTTL)
		return {taskCount, ''}
	`)
	response, err := script.Run(ctx, s.rdb, []string{markerKey, s.processingKey, taskQueueKeysSet}, args...).Result()
	if err != nil {
		return false, err
	}
	values, ok := response.([]interface{})
	if !ok || len(values) != 2 {
		return false, fmt.Errorf("unexpected recovery result %T", response)
	}
	code, err := redisResultInt(values[0])
	if err != nil {
		return false, err
	}
	switch code {
	case -1:
		taskID, _ := redisResultString(values[1])
		return false, fmt.Errorf("%w: %s", ErrTaskLeaseConflict, taskID)
	case -2:
		return false, ErrTaskOperationBusy
	case -3:
		return false, fmt.Errorf("requeue marker manifest mismatch")
	}
	if code > 0 {
		s.notifyTaskAvailable(ctx)
	}
	return true, nil
}

// RequeueExactTask is the single-child compatibility wrapper.
func (s *Scheduler) RequeueExactTask(ctx context.Context, task *TaskInfo) (bool, error) {
	return s.RequeueExactTaskBatch(ctx, []*TaskInfo{task})
}

var cleanupUnownedProcessingScript = redis.NewScript(`
	if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 0 then
		return 0
	end
	if redis.call('EXISTS', KEYS[2]) == 1 or redis.call('EXISTS', KEYS[3]) == 1 or redis.call('EXISTS', KEYS[5]) == 1 then
		return 0
	end
	if ARGV[2] ~= '' then
		local status = redis.call('GET', KEYS[4])
		if status then
			local decoded = nil
			pcall(function() decoded = cjson.decode(status) end)
			if decoded and decoded.worker and decoded.worker ~= '' and decoded.worker ~= ARGV[2] then
				return 0
			end
		end
	end
	redis.call('SREM', KEYS[1], ARGV[1])
	redis.call('DEL', KEYS[4])
	return 1
`)

// CleanupUnownedProcessingTask atomically proves that no payload or execution
// appeared before deleting an unrecoverable processing marker.
func (s *Scheduler) CleanupUnownedProcessingTask(ctx context.Context, taskID, workerName string) (bool, error) {
	result, err := cleanupUnownedProcessingScript.Run(ctx, s.rdb, []string{
		s.processingKey,
		taskInfoKeyPrefix + taskID,
		taskExecutionKeyPrefix + taskID,
		taskStatusKeyPrefix + taskID,
		taskOperationGuardKeyPrefix + taskID,
	}, taskID, workerName).Int()
	return result == 1, err
}
