package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	// TaskProtocolV1 identifies execution records that are fenced by both a
	// process instance heartbeat and a per-acquisition lease token.
	TaskProtocolV1 = 1

	taskInfoKeyPrefix      = "cscan:task:info:"
	taskExecutionKeyPrefix = "cscan:task:execution:"
	taskQueuedKeyPrefix    = "cscan:task:queued:"
	taskQueueKeysSet       = "cscan:task:queue:keys"
	taskStatusKeyPrefix    = "cscan:task:status:"
	taskProgressKeyPrefix  = "cscan:task:progress:"
	taskCompletedSet       = "cscan:task:completed"
	taskMetadataTTL        = 24 * time.Hour
)

var (
	ErrTaskLeaseConflict     = errors.New("task execution lease is no longer owned")
	ErrTaskParentFenced      = errors.New("task execution is fenced by a same-generation parent control")
	ErrTaskOperationBusy     = errors.New("task execution has a guarded operation in progress")
	ErrTaskAlreadyProcessing = errors.New("task is already processing")
)

type queuedTaskEntry struct {
	taskID string
	queue  string
	score  float64
	member string
}

// leasedPopScript is the single acquisition primitive for direct and HTTP
// workers. A unique token fences every later state transition. The queued
// location hash is consumed with the pop so duplicate affinity copies cannot
// be acquired by a second worker.
var leasedPopScript = redis.NewScript(`
	local workerQueueKey = KEYS[1]
	local queueKey = KEYS[2]
	local processingKey = KEYS[3]
	local useWorkerQueue = ARGV[1] == '1'
	local useBuckets = ARGV[2] == '1'
	local taskInfoPrefix = ARGV[3]
	local executionPrefix = ARGV[4]
	local queuedPrefix = ARGV[5]
	local workerName = ARGV[6]
	local instanceID = ARGV[7]
	local taskProtocol = tonumber(ARGV[8])
	local metadataTTL = tonumber(ARGV[9])
	local nowString = ARGV[10]
	local nowUnix = tonumber(ARGV[11])
	local leaseToken = ARGV[12]

	local function popNext()
		local result = nil
		if useWorkerQueue then
			result = redis.call('ZPOPMIN', workerQueueKey, 1)
			if #result > 0 then
				return result
			end
		end
		if useBuckets then
			for priority = 4, 0, -1 do
				result = redis.call('ZPOPMIN', queueKey .. ':p' .. priority, 1)
				if #result > 0 then
					return result
				end
			end
			return {}
		end
		return redis.call('ZPOPMIN', queueKey, 1)
	end

	local function scrubQueuedCopies(taskID)
		local locationKey = queuedPrefix .. taskID
		local locations = redis.call('HGETALL', locationKey)
		for index = 1, #locations, 2 do
			redis.call('ZREM', locations[index], locations[index + 1])
		end
		redis.call('DEL', locationKey)
	end

	for attempt = 1, 100 do
		local result = popNext()
		if #result == 0 then
			return nil
		end
		local member = result[1]
		local score = result[2]
		local task = nil
		pcall(function() task = cjson.decode(member) end)
		if not task or not task.taskId or task.taskId == '' then
			redis.call('ZADD', 'cscan:task:deadletter', score, member)
			return {'__DL__' .. member, ''}
		end

		local taskID = task.taskId
		local executionKey = executionPrefix .. taskID
		if redis.call('SISMEMBER', processingKey, taskID) == 1 or redis.call('EXISTS', executionKey) == 1 then
			-- This is a duplicate queued copy of an active acquisition. Remove all
			-- indexed copies and continue without handing it to another worker.
			scrubQueuedCopies(taskID)
		else
			scrubQueuedCopies(taskID)
			redis.call('SADD', processingKey, taskID)
			redis.call('SET', taskInfoPrefix .. taskID, member, 'EX', metadataTTL)
			local execution = cjson.encode({
				taskId = taskID,
				workerName = workerName,
				instanceId = instanceID,
				taskProtocol = taskProtocol,
				dispatchGeneration = task.dispatchGeneration or '',
				leaseToken = leaseToken,
				startTime = nowString,
				lastUpdate = nowString,
				lastUpdateUnix = nowUnix,
				phase = 'started',
				progress = 0,
				retryCount = 0,
				maxRetries = 3
			})
			redis.call('SET', executionKey, execution, 'EX', metadataTTL)
			return {member, leaseToken}
		end
	end
	return nil
`)

func (s *Scheduler) popLeasedTask(
	ctx context.Context,
	workerName, instanceID string,
	taskProtocol int,
	useWorkerQueue bool,
) (*TaskInfo, error) {
	leaseToken := uuid.NewString()
	workerQueueKey := s.queueKey
	if useWorkerQueue {
		workerQueueKey = s.GetWorkerQueueKey(workerName)
	}
	bucketFlag := "0"
	if s.enablePriorityBucket.Load() {
		bucketFlag = "1"
	}
	workerFlag := "0"
	if useWorkerQueue {
		workerFlag = "1"
	}

	now := time.Now().UTC()
	result, err := leasedPopScript.Run(ctx, s.rdb,
		[]string{workerQueueKey, s.queueKey, s.processingKey},
		workerFlag,
		bucketFlag,
		taskInfoKeyPrefix,
		taskExecutionKeyPrefix,
		taskQueuedKeyPrefix,
		workerName,
		instanceID,
		taskProtocol,
		int(taskMetadataTTL/time.Second),
		now.Format(time.RFC3339Nano),
		now.Unix(),
		leaseToken,
	).Result()
	if err == redis.Nil || result == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return nil, fmt.Errorf("unexpected leased pop result type %T", result)
	}
	member, ok := redisResultString(values[0])
	if !ok {
		return nil, fmt.Errorf("unexpected leased pop payload type %T", values[0])
	}
	if strings.HasPrefix(member, "__DL__") {
		s.publishDeadLetterAlert(ctx, strings.TrimPrefix(member, "__DL__"))
		return nil, nil
	}
	returnedLease, ok := redisResultString(values[1])
	if !ok || returnedLease == "" {
		return nil, fmt.Errorf("leased pop returned an empty lease token")
	}

	var task TaskInfo
	if err := json.Unmarshal([]byte(member), &task); err != nil {
		return nil, err
	}
	task.LeaseToken = returnedLease
	return &task, nil
}

func redisResultString(value interface{}) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case []byte:
		return string(typed), true
	default:
		return "", false
	}
}

func hasMongoParentID(value string) bool {
	if len(value) != 24 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func (s *Scheduler) buildQueuedTaskEntries(tasks []*TaskInfo, resetCreateTime bool) ([]queuedTaskEntry, []string, error) {
	if len(tasks) == 0 {
		return nil, nil, nil
	}
	baseTime := time.Now()
	entries := make([]queuedTaskEntry, 0, len(tasks))
	taskIDs := make([]string, 0, len(tasks))
	seenTaskIDs := make(map[string]struct{}, len(tasks))

	for index, task := range tasks {
		if task == nil {
			return nil, nil, fmt.Errorf("queued task cannot be nil")
		}
		if task.TaskId == "" {
			task.TaskId = uuid.NewString()
		}
		if resetCreateTime || strings.TrimSpace(task.CreateTime) == "" {
			task.CreateTime = baseTime.Local().Format("2006-01-02 15:04:05")
		}
		if _, exists := seenTaskIDs[task.TaskId]; !exists {
			seenTaskIDs[task.TaskId] = struct{}{}
			taskIDs = append(taskIDs, task.TaskId)
		}
		score := s.calculatePriorityScore(task.Priority, baseTime.Add(time.Duration(index)*time.Microsecond))

		if len(task.Workers) > 0 {
			for _, workerName := range task.Workers {
				taskCopy := *task
				taskCopy.Workers = []string{workerName}
				data, err := json.Marshal(taskCopy)
				if err != nil {
					return nil, nil, fmt.Errorf("marshal task %s for worker %s: %w", task.TaskId, workerName, err)
				}
				entries = append(entries, queuedTaskEntry{
					taskID: task.TaskId,
					queue:  s.GetWorkerQueueKey(workerName),
					score:  score,
					member: string(data),
				})
			}
			continue
		}

		data, err := json.Marshal(task)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal task %s: %w", task.TaskId, err)
		}
		queueKey := s.queueKey
		if s.enablePriorityBucket.Load() {
			queueKey = priorityBucketKey(task.Priority)
		}
		entries = append(entries, queuedTaskEntry{taskID: task.TaskId, queue: queueKey, score: score, member: string(data)})
	}
	return entries, taskIDs, nil
}

var publishTaskBatchScript = redis.NewScript(`
	local processingKey = KEYS[1]
	local queueKeysSet = KEYS[2]
	local markerKey = KEYS[3]
	local locationPrefix = ARGV[1]
	local executionPrefix = ARGV[2]
	local taskCount = tonumber(ARGV[3])
	local entryCount = tonumber(ARGV[4])
	local useMarker = ARGV[5] == '1'
	local operationPrefix = ARGV[#ARGV - 2]
	local markerValue = ARGV[#ARGV - 1]
	local markerTTL = tonumber(ARGV[#ARGV])

	if useMarker then
		local existingMarker = redis.call('GET', markerKey)
		if existingMarker then
			if existingMarker == markerValue then
				return 0
			end
			return -3
		end
	end

	for index = 1, taskCount do
		local taskID = ARGV[5 + index]
		if redis.call('EXISTS', operationPrefix .. taskID) == 1 then
			return -2
		end
		if redis.call('SISMEMBER', processingKey, taskID) == 1 or redis.call('EXISTS', executionPrefix .. taskID) == 1 then
			return -1
		end
	end

	for index = 1, taskCount do
		local taskID = ARGV[5 + index]
		local locationKey = locationPrefix .. taskID
		local locations = redis.call('HGETALL', locationKey)
		for locationIndex = 1, #locations, 2 do
			redis.call('ZREM', locations[locationIndex], locations[locationIndex + 1])
		end
		redis.call('DEL', locationKey)
	end

	local entryBase = 6 + taskCount
	for index = 1, entryCount do
		local offset = entryBase + ((index - 1) * 4)
		local taskID = ARGV[offset]
		local queueKey = ARGV[offset + 1]
		local score = ARGV[offset + 2]
		local member = ARGV[offset + 3]
		redis.call('ZADD', queueKey, score, member)
		redis.call('HSET', locationPrefix .. taskID, queueKey, member)
		redis.call('SADD', queueKeysSet, queueKey)
	end

	if useMarker then
		-- Publication markers are bounded crash/idempotency records, not locks.
		-- Existing-marker retries return above and never refresh this 24h TTL.
		redis.call('SET', markerKey, markerValue, 'EX', markerTTL)
	end
	return entryCount
`)

func publicationMarkerValue(markerKey string, entries []queuedTaskEntry) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte("cscan-publication-v1\x00" + markerKey))
	for _, entry := range entries {
		_, _ = digest.Write([]byte("\x01" + entry.taskID + "\x00" + entry.queue + "\x00" + entry.member))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func (s *Scheduler) publishTaskBatch(ctx context.Context, tasks []*TaskInfo, markerKey string, resetCreateTime bool) error {
	if len(tasks) == 0 {
		return nil
	}
	entries, taskIDs, err := s.buildQueuedTaskEntries(tasks, resetCreateTime)
	if err != nil {
		return err
	}
	useMarker := "1"
	scriptMarkerKey := markerKey
	if scriptMarkerKey == "" {
		useMarker = "0"
		scriptMarkerKey = "cscan:task:publish:unmarked"
	}
	args := make([]interface{}, 0, 8+len(taskIDs)+4*len(entries))
	args = append(args, taskQueuedKeyPrefix, taskExecutionKeyPrefix, len(taskIDs), len(entries), useMarker)
	for _, taskID := range taskIDs {
		args = append(args, taskID)
	}
	for _, entry := range entries {
		args = append(args, entry.taskID, entry.queue, entry.score, entry.member)
	}
	args = append(args, taskOperationGuardKeyPrefix, publicationMarkerValue(markerKey, entries), int(taskMetadataTTL/time.Second))
	result, err := publishTaskBatchScript.Run(ctx, s.rdb,
		[]string{s.processingKey, taskQueueKeysSet, scriptMarkerKey}, args...).Int()
	if err != nil {
		return err
	}
	switch result {
	case -1:
		return ErrTaskAlreadyProcessing
	case -2:
		return ErrTaskOperationBusy
	case -3:
		return fmt.Errorf("publication marker manifest mismatch")
	}
	if result > 0 {
		s.notifyTaskAvailable(ctx)
	}
	return nil
}

// PushTaskBatchOnce atomically publishes a stable batch exactly once. The
// caller must persist the exact manifest first and reuse markerKey and payload
// for every retry after an ambiguous Redis response.
func (s *Scheduler) PushTaskBatchOnce(ctx context.Context, tasks []*TaskInfo, markerKey string) error {
	if markerKey == "" {
		return fmt.Errorf("task publication marker cannot be empty")
	}
	for _, task := range tasks {
		if task == nil || strings.TrimSpace(task.CreateTime) == "" {
			return fmt.Errorf("idempotent task publication requires stable create times")
		}
	}
	return s.publishTaskBatch(ctx, tasks, markerKey, false)
}
