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

	"github.com/redis/go-redis/v9"
)

var discardExactTaskScript = redis.NewScript(`
	if redis.call('EXISTS', KEYS[6]) == 1 then
		return -2
	end
	if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 0 then
		return -1
	end
	local execution = redis.call('GET', KEYS[2])
	if not execution then
		return -1
	end
	local decoded = nil
	pcall(function() decoded = cjson.decode(execution) end)
	if not decoded or (decoded.taskId or '') ~= ARGV[1] or (decoded.leaseToken or '') ~= ARGV[2] or
		(decoded.dispatchGeneration or '') ~= ARGV[3] then
		return -1
	end
	redis.call('SREM', KEYS[1], ARGV[1])
	redis.call('DEL', KEYS[2], KEYS[3], KEYS[4], KEYS[5])
	return 1
`)

// DiscardExactTask removes a just-acquired terminal/superseded generation. It
// never removes a different lease or one with an active cross-store operation.
func (s *Scheduler) DiscardExactTask(ctx context.Context, task *TaskInfo) error {
	if task == nil || task.TaskId == "" || task.LeaseToken == "" {
		return ErrTaskLeaseConflict
	}
	result, err := discardExactTaskScript.Run(ctx, s.rdb, []string{
		s.processingKey,
		taskExecutionKeyPrefix + task.TaskId,
		taskInfoKeyPrefix + task.TaskId,
		taskStatusKeyPrefix + task.TaskId,
		taskProgressKeyPrefix + task.TaskId,
		taskOperationGuardKeyPrefix + task.TaskId,
	}, task.TaskId, task.LeaseToken, task.DispatchGeneration).Int()
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

type DeadTaskResolutionAction string

const (
	DeadTaskResolutionRequeue  DeadTaskResolutionAction = "requeue"
	DeadTaskResolutionPause    DeadTaskResolutionAction = "pause"
	DeadTaskResolutionDiscard  DeadTaskResolutionAction = "discard"
	DeadTaskResolutionComplete DeadTaskResolutionAction = "complete"
)

type DeadTaskResolution struct {
	Task          *TaskInfo
	ExpectedPhase string
	Action        DeadTaskResolutionAction
	Worker        string
	Phase         string
	Operation     *LeaseOperation
}

type deadTaskResolutionOutput struct {
	status    string
	progress  string
	completed string
}

func deadResolutionMarker(resolutions []DeadTaskResolution) string {
	parts := make([]string, 0, len(resolutions))
	for _, resolution := range resolutions {
		if resolution.Task == nil {
			continue
		}
		parts = append(parts, resolution.Task.TaskId+"\x00"+resolution.Task.DispatchGeneration+"\x00"+
			resolution.Task.LeaseToken+"\x00"+resolution.ExpectedPhase+"\x00"+string(resolution.Action))
	}
	sort.Strings(parts)
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return "cscan:task:resolve-pausing:" + hex.EncodeToString(digest[:])
}

func deadResolutionMarkerValues(
	markerKey string,
	resolutions []DeadTaskResolution,
	outputs []deadTaskResolutionOutput,
	entries []queuedTaskEntry,
) (string, string) {
	legacyDigest := sha256.New()
	_, _ = legacyDigest.Write([]byte("cscan-dead-pausing-v1\x00" + markerKey))
	for _, resolution := range resolutions {
		if resolution.Task != nil {
			_, _ = legacyDigest.Write([]byte("\x01" + resolution.Task.TaskId + "\x00" + string(resolution.Action)))
		}
	}
	for _, entry := range entries {
		_, _ = legacyDigest.Write([]byte("\x02" + entry.taskID + "\x00" + entry.queue + "\x00" + entry.member))
	}

	manifest := make([]string, 0, len(resolutions)+len(entries))
	for index, resolution := range resolutions {
		if resolution.Task == nil {
			continue
		}
		output := outputs[index]
		manifest = append(manifest, "task\x00"+resolution.Task.TaskId+"\x00"+
			resolution.Task.DispatchGeneration+"\x00"+resolution.Task.LeaseToken+"\x00"+
			resolution.ExpectedPhase+"\x00"+string(resolution.Action)+"\x00"+resolution.Worker+"\x00"+
			resolution.Phase+"\x00"+output.status+"\x00"+output.progress+"\x00"+output.completed)
	}
	for _, entry := range entries {
		manifest = append(manifest, fmt.Sprintf("queue\x00%s\x00%s\x00%g\x00%s",
			entry.taskID, entry.queue, entry.score, entry.member))
	}
	sort.Strings(manifest)
	digest := sha256.Sum256([]byte("cscan-dead-resolution-v2\x00" + markerKey + "\x01" + strings.Join(manifest, "\x01")))
	return hex.EncodeToString(digest[:]), hex.EncodeToString(legacyDigest.Sum(nil))
}

// resolveDeadTaskBatch revalidates the complete exact v1 owner/payload set,
// absent instance heartbeats, and either absent or exactly-held operation
// guards before changing any member, preserving parent-group all-or-none
// recovery. A complete action emits the same retained closure as a normal
// exact-lease COMPLETED transition.
func (s *Scheduler) resolveDeadTaskBatch(ctx context.Context, resolutions []DeadTaskResolution, requirePausing bool) (bool, error) {
	if len(resolutions) == 0 {
		return false, fmt.Errorf("dead task resolution cannot be empty")
	}
	seen := make(map[string]struct{}, len(resolutions))
	hasPausing := false
	requeueTasks := make([]*TaskInfo, 0, len(resolutions))
	outputs := make([]deadTaskResolutionOutput, len(resolutions))
	for index, resolution := range resolutions {
		task := resolution.Task
		if task == nil || task.TaskId == "" || task.MainTaskId == "" || task.Config == "" ||
			task.LeaseToken == "" || task.RecoveryInstanceID == "" || task.DispatchGeneration == "" {
			return false, fmt.Errorf("dead task resolution requires exact payload, owner, lease, and dispatch generation")
		}
		if _, duplicate := seen[task.TaskId]; duplicate {
			return false, fmt.Errorf("duplicate dead task %s", task.TaskId)
		}
		seen[task.TaskId] = struct{}{}
		if strings.EqualFold(strings.TrimSpace(resolution.ExpectedPhase), "pausing") {
			hasPausing = true
		}
		if operation := resolution.Operation; operation != nil {
			if operation.AlreadyClosed || operation.guardValue == "" || operation.TaskID != task.TaskId ||
				operation.LeaseToken != task.LeaseToken || operation.WorkerName != resolution.Worker ||
				operation.InstanceID != task.RecoveryInstanceID || operation.TaskProtocol != TaskProtocolV1 ||
				operation.DispatchGeneration != task.DispatchGeneration || operation.TaskInfoData == "" {
				return false, ErrTaskLeaseConflict
			}
		}
		switch resolution.Action {
		case DeadTaskResolutionRequeue:
			prepared, err := s.prepareRecoveredTask(ctx, task)
			if err != nil {
				return false, err
			}
			requeueTasks = append(requeueTasks, prepared)
		case DeadTaskResolutionPause:
			statusBytes, err := json.Marshal(map[string]interface{}{
				"taskId": task.TaskId, "state": "PAUSED", "worker": resolution.Worker,
				"phase": resolution.Phase, "leaseToken": task.LeaseToken,
				"dispatchGeneration": task.DispatchGeneration,
			})
			if err != nil {
				return false, err
			}
			outputs[index].status = string(statusBytes)
			if resolution.Phase != "" {
				progressBytes, err := json.Marshal(map[string]interface{}{
					"currentPhase": resolution.Phase, "leaseToken": task.LeaseToken,
					"dispatchGeneration": task.DispatchGeneration,
				})
				if err != nil {
					return false, err
				}
				outputs[index].progress = string(progressBytes)
			}
		case DeadTaskResolutionComplete:
			if strings.TrimSpace(resolution.Worker) == "" {
				return false, fmt.Errorf("completed dead task %s requires its exact worker", task.TaskId)
			}
			statusBytes, err := json.Marshal(map[string]interface{}{
				"taskId": task.TaskId, "state": "COMPLETED", "worker": resolution.Worker,
				"result": "", "phase": resolution.Phase, "leaseToken": task.LeaseToken,
				"dispatchGeneration": task.DispatchGeneration,
			})
			if err != nil {
				return false, err
			}
			outputs[index].status = string(statusBytes)
			if resolution.Phase != "" {
				progressBytes, err := json.Marshal(map[string]interface{}{
					"currentPhase": resolution.Phase, "leaseToken": task.LeaseToken,
					"dispatchGeneration": task.DispatchGeneration,
				})
				if err != nil {
					return false, err
				}
				outputs[index].progress = string(progressBytes)
			}
			completedBytes, err := json.Marshal(TaskInfo{
				TaskId: task.TaskId, DispatchGeneration: task.DispatchGeneration,
			})
			if err != nil {
				return false, err
			}
			outputs[index].completed = string(completedBytes)
		case DeadTaskResolutionDiscard:
		default:
			return false, fmt.Errorf("unknown dead task resolution action %q", resolution.Action)
		}
	}
	if requirePausing && !hasPausing {
		return false, fmt.Errorf("dedicated pausing resolution requires a pausing candidate")
	}

	entries, requeueIDs, err := s.buildQueuedTaskEntries(requeueTasks, false)
	if err != nil {
		return false, err
	}
	if len(entries) != len(requeueIDs) || len(requeueIDs) != len(requeueTasks) {
		return false, fmt.Errorf("dead task requeue plan must have exactly one destination per task")
	}

	markerKey := deadResolutionMarker(resolutions)
	markerValue, legacyMarkerValue := deadResolutionMarkerValues(markerKey, resolutions, outputs, entries)
	args := make([]interface{}, 0, 12+11*len(resolutions)+4*len(entries))
	args = append(args, taskQueuedKeyPrefix, taskExecutionKeyPrefix, taskInfoKeyPrefix,
		taskStatusKeyPrefix, taskProgressKeyPrefix, len(resolutions), len(entries))
	for index, resolution := range resolutions {
		task := resolution.Task
		expectedGuard := ""
		if resolution.Operation != nil {
			expectedGuard = resolution.Operation.guardValue
		}
		output := outputs[index]
		args = append(args, task.TaskId, task.LeaseToken, task.RecoveryInstanceID,
			task.DispatchGeneration, resolution.ExpectedPhase, string(resolution.Action), resolution.Worker,
			expectedGuard, output.status, output.progress, output.completed)
	}
	for _, entry := range entries {
		args = append(args, entry.taskID, entry.queue, entry.score, entry.member)
	}
	args = append(args, taskOperationGuardKeyPrefix, "cscan:worker:instance:",
		markerValue, legacyMarkerValue, int(taskMetadataTTL/time.Second))

	script := redis.NewScript(`
		local queuedPrefix = ARGV[1]
		local executionPrefix = ARGV[2]
		local infoPrefix = ARGV[3]
		local statusPrefix = ARGV[4]
		local progressPrefix = ARGV[5]
		local taskCount = tonumber(ARGV[6])
		local entryCount = tonumber(ARGV[7])
		local operationPrefix = ARGV[#ARGV - 4]
		local heartbeatPrefix = ARGV[#ARGV - 3]
		local markerValue = ARGV[#ARGV - 2]
		local legacyMarkerValue = ARGV[#ARGV - 1]
		local metadataTTL = tonumber(ARGV[#ARGV])

		local existingMarker = redis.call('GET', KEYS[1])
		if existingMarker then
			if existingMarker == markerValue or existingMarker == legacyMarkerValue then
				return {0, ''}
			end
			return {-3, ''}
		end

		for index = 1, taskCount do
			local offset = 8 + ((index - 1) * 11)
			local taskID = ARGV[offset]
			local expectedLease = ARGV[offset + 1]
			local expectedInstance = ARGV[offset + 2]
			local expectedDispatch = ARGV[offset + 3]
			local expectedPhase = ARGV[offset + 4]
			local action = ARGV[offset + 5]
			local expectedWorker = ARGV[offset + 6]
			local expectedGuard = ARGV[offset + 7]
			local statusData = ARGV[offset + 8]
			local completedData = ARGV[offset + 10]
			if action ~= 'requeue' and action ~= 'pause' and action ~= 'discard' and action ~= 'complete' then
				return {-1, taskID}
			end
			if action == 'complete' and (expectedWorker == '' or statusData == '' or completedData == '') then
				return {-1, taskID}
			end
			local currentGuard = redis.call('GET', operationPrefix .. taskID)
			if expectedGuard ~= '' then
				if currentGuard ~= expectedGuard then
					return {-2, taskID}
				end
			elseif currentGuard then
				return {-2, taskID}
			end
			if redis.call('SISMEMBER', KEYS[2], taskID) == 0 then
				return {-1, taskID}
			end
			local execution = redis.call('GET', executionPrefix .. taskID)
			local decoded = nil
			if execution then
				pcall(function() decoded = cjson.decode(execution) end)
			end
			if not decoded or (decoded.taskId or '') ~= taskID or
				(decoded.leaseToken or '') ~= expectedLease or
				(decoded.instanceId or '') ~= expectedInstance or
				tonumber(decoded.taskProtocol or 0) ~= 1 or
				(decoded.dispatchGeneration or '') ~= expectedDispatch or
				(decoded.phase or '') ~= expectedPhase or
				(expectedWorker ~= '' and (decoded.workerName or '') ~= expectedWorker) then
				return {-1, taskID}
			end
			if redis.call('EXISTS', heartbeatPrefix .. expectedInstance) == 1 then
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
		end

		for index = 1, taskCount do
			local offset = 8 + ((index - 1) * 11)
			local taskID = ARGV[offset]
			local locationKey = queuedPrefix .. taskID
			local locations = redis.call('HGETALL', locationKey)
			for locationIndex = 1, #locations, 2 do
				redis.call('ZREM', locations[locationIndex], locations[locationIndex + 1])
			end
			redis.call('DEL', locationKey)
		end

		local entryBase = 8 + (taskCount * 11)
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
			local offset = 8 + ((index - 1) * 11)
			local taskID = ARGV[offset]
			local action = ARGV[offset + 5]
			local expectedGuard = ARGV[offset + 7]
			local statusData = ARGV[offset + 8]
			local progressData = ARGV[offset + 9]
			local completedData = ARGV[offset + 10]
			if action == 'pause' then
				redis.call('SET', statusPrefix .. taskID, statusData, 'EX', metadataTTL)
				if progressData ~= '' then
					redis.call('SET', progressPrefix .. taskID, progressData, 'EX', metadataTTL)
				end
			elseif action == 'complete' then
				redis.call('SET', statusPrefix .. taskID, statusData, 'EX', metadataTTL)
				if progressData ~= '' then
					redis.call('SET', progressPrefix .. taskID, progressData, 'EX', metadataTTL)
				end
				redis.call('EXPIRE', infoPrefix .. taskID, metadataTTL)
				redis.call('SADD', KEYS[4], completedData)
			elseif action == 'discard' then
				redis.call('DEL', infoPrefix .. taskID, statusPrefix .. taskID, progressPrefix .. taskID)
			else
				redis.call('DEL', statusPrefix .. taskID, progressPrefix .. taskID)
			end
			redis.call('SREM', KEYS[2], taskID)
			redis.call('DEL', executionPrefix .. taskID)
			if expectedGuard ~= '' then
				redis.call('DEL', operationPrefix .. taskID)
			end
		end
		redis.call('SET', KEYS[1], markerValue, 'EX', metadataTTL)
		return {taskCount, ''}
	`)
	response, err := script.Run(ctx, s.rdb,
		[]string{markerKey, s.processingKey, taskQueueKeysSet, taskCompletedSet}, args...).Result()
	if err != nil {
		return false, err
	}
	values, ok := response.([]interface{})
	if !ok || len(values) != 2 {
		return false, fmt.Errorf("unexpected dead task resolution result %T", response)
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
		return false, fmt.Errorf("dead task marker manifest mismatch")
	}
	if code > 0 && len(requeueTasks) > 0 {
		s.notifyTaskAvailable(ctx)
	}
	return true, nil
}

// ResolveDeadTaskBatch applies an explicit all-or-none dead-owner plan. It is
// used when terminal, completed, or superseded parent state requires a mixed
// exact transition rather than a runnable all-requeue shortcut.
func (s *Scheduler) ResolveDeadTaskBatch(ctx context.Context, resolutions []DeadTaskResolution) (bool, error) {
	return s.resolveDeadTaskBatch(ctx, resolutions, false)
}

// ResolveDeadPausingTaskBatch is the only generic-recovery escape hatch for a
// phase=pausing lease; at least one exact pausing candidate is required.
func (s *Scheduler) ResolveDeadPausingTaskBatch(ctx context.Context, resolutions []DeadTaskResolution) (bool, error) {
	return s.resolveDeadTaskBatch(ctx, resolutions, true)
}
