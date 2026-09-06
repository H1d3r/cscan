package scheduler

import (
	"context"
	"fmt"
)

// IsTaskBatchQuiescent atomically proves that no canonical child has an
// execution, processing membership, or cross-store operation in flight. A
// PAUSED parent remains the acquisition fence while resume performs this check
// before selecting and publishing the next durable generation.
func (s *Scheduler) IsTaskBatchQuiescent(ctx context.Context, taskIDs []string) (bool, error) {
	if len(taskIDs) == 0 {
		return false, fmt.Errorf("task batch quiescence requires canonical child ids")
	}
	seen := make(map[string]struct{}, len(taskIDs))
	args := make([]interface{}, 0, len(taskIDs)+3)
	args = append(args, len(taskIDs))
	for _, taskID := range taskIDs {
		if taskID == "" {
			return false, fmt.Errorf("task batch quiescence contains an empty child id")
		}
		if _, duplicate := seen[taskID]; duplicate {
			return false, fmt.Errorf("task batch quiescence contains duplicate child %s", taskID)
		}
		seen[taskID] = struct{}{}
		args = append(args, taskID)
	}
	args = append(args, taskExecutionKeyPrefix, taskOperationGuardKeyPrefix)
	script := `
		local taskCount = tonumber(ARGV[1])
		local executionPrefix = ARGV[#ARGV - 1]
		local operationPrefix = ARGV[#ARGV]
		for index = 1, taskCount do
			local taskID = ARGV[1 + index]
			if redis.call('SISMEMBER', KEYS[1], taskID) == 1 or
				redis.call('EXISTS', executionPrefix .. taskID) == 1 or
				redis.call('EXISTS', operationPrefix .. taskID) == 1 then
				return {0, taskID}
			end
		end
		return {1, ''}
	`
	response, err := s.rdb.Eval(ctx, script, []string{s.processingKey}, args...).Result()
	if err != nil {
		return false, err
	}
	values, ok := response.([]interface{})
	if !ok || len(values) != 2 {
		return false, fmt.Errorf("unexpected task quiescence result %T", response)
	}
	code, err := redisResultInt(values[0])
	if err != nil {
		return false, err
	}
	return code == 1, nil
}
