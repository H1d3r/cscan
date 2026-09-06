package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	TaskControlActionPause = "PAUSE"
	TaskControlActionStop  = "STOP"

	taskControlKeyPrefix = "cscan:task:ctrl:"
	taskControlTTL       = 24 * time.Hour
)

var ErrTaskControlSuperseded = errors.New("task control was superseded by STOP")

// TaskControlTarget is the exact execution identity used by durable polling.
// A task ID without its dispatch generation is never a valid v1 control target.
type TaskControlTarget struct {
	TaskID             string `json:"taskId"`
	DispatchGeneration string `json:"dispatchGeneration"`
}

func (target TaskControlTarget) Validate() error {
	if strings.TrimSpace(target.TaskID) == "" || strings.TrimSpace(target.DispatchGeneration) == "" {
		return fmt.Errorf("task control target requires taskId and dispatchGeneration")
	}
	return nil
}

// TaskControlEnvelope is the only task-level control representation accepted
// by v1 workers on Redis, HTTP, WebSocket, and in local worker storage.
type TaskControlEnvelope struct {
	IntentID           string    `json:"intentId"`
	MainTaskID         string    `json:"mainTaskId"`
	TaskID             string    `json:"taskId"`
	Action             string    `json:"action"`
	DispatchGeneration string    `json:"dispatchGeneration"`
	Timestamp          time.Time `json:"timestamp"`
}

func (envelope TaskControlEnvelope) Validate() error {
	if strings.TrimSpace(envelope.IntentID) == "" || strings.TrimSpace(envelope.MainTaskID) == "" ||
		strings.TrimSpace(envelope.TaskID) == "" || strings.TrimSpace(envelope.DispatchGeneration) == "" ||
		envelope.Timestamp.IsZero() {
		return fmt.Errorf("task control envelope is incomplete")
	}
	if envelope.Action != TaskControlActionPause && envelope.Action != TaskControlActionStop {
		return fmt.Errorf("unsupported task control action %q", envelope.Action)
	}
	return nil
}

func (envelope TaskControlEnvelope) Target() TaskControlTarget {
	return TaskControlTarget{TaskID: envelope.TaskID, DispatchGeneration: envelope.DispatchGeneration}
}

func (envelope TaskControlEnvelope) Key() (string, error) {
	if err := envelope.Validate(); err != nil {
		return "", err
	}
	return TaskControlKey(envelope.TaskID, envelope.DispatchGeneration)
}

func TaskControlKey(taskID, dispatchGeneration string) (string, error) {
	target := TaskControlTarget{TaskID: taskID, DispatchGeneration: dispatchGeneration}
	if err := target.Validate(); err != nil {
		return "", err
	}
	return taskControlKeyPrefix + taskID + ":" + dispatchGeneration, nil
}

func TaskControlChannelPattern() string {
	return taskControlKeyPrefix + "*:*"
}

func marshalTaskControlEnvelope(envelope TaskControlEnvelope) ([]byte, error) {
	envelope.IntentID = strings.TrimSpace(envelope.IntentID)
	envelope.MainTaskID = strings.TrimSpace(envelope.MainTaskID)
	envelope.TaskID = strings.TrimSpace(envelope.TaskID)
	envelope.DispatchGeneration = strings.TrimSpace(envelope.DispatchGeneration)
	envelope.Timestamp = envelope.Timestamp.UTC().Truncate(time.Millisecond)
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(envelope)
}

// ParseTaskControlEnvelope rejects plaintext, missing generations, unknown
// fields, and trailing JSON. Controls never fall back to a task-ID-only form.
func ParseTaskControlEnvelope(data []byte) (*TaskControlEnvelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope TaskControlEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode task control envelope: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("decode task control envelope: trailing JSON value")
		}
		return nil, fmt.Errorf("decode task control envelope trailing data: %w", err)
	}
	envelope.IntentID = strings.TrimSpace(envelope.IntentID)
	envelope.MainTaskID = strings.TrimSpace(envelope.MainTaskID)
	envelope.TaskID = strings.TrimSpace(envelope.TaskID)
	envelope.DispatchGeneration = strings.TrimSpace(envelope.DispatchGeneration)
	envelope.Timestamp = envelope.Timestamp.UTC().Truncate(time.Millisecond)
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	return &envelope, nil
}

var publishTaskControlScript = redis.NewScript(`
	local incomingRaw = ARGV[1]
	local ttl = tonumber(ARGV[2])
	local incoming = nil
	local incomingOK = pcall(function() incoming = cjson.decode(incomingRaw) end)
	if not incomingOK or not incoming or (incoming.intentId or '') == '' or
		(incoming.mainTaskId or '') == '' or (incoming.taskId or '') == '' or
		(incoming.dispatchGeneration or '') == '' or (incoming.timestamp or '') == '' or
		((incoming.action or '') ~= 'PAUSE' and (incoming.action or '') ~= 'STOP') then
		return -4
	end

	local existingRaw = redis.call('GET', KEYS[1])
	if not existingRaw then
		redis.call('SET', KEYS[1], incomingRaw, 'EX', ttl)
		redis.call('PUBLISH', KEYS[1], incomingRaw)
		return 1
	end
	if existingRaw == incomingRaw then
		-- A replay repairs missed Pub/Sub delivery without extending retention.
		redis.call('PUBLISH', KEYS[1], incomingRaw)
		return 0
	end

	local existing = nil
	local existingOK = pcall(function() existing = cjson.decode(existingRaw) end)
	if not existingOK or not existing then
		return -3
	end
	if (existing.mainTaskId or '') ~= (incoming.mainTaskId or '') or
		(existing.taskId or '') ~= (incoming.taskId or '') or
		(existing.dispatchGeneration or '') ~= (incoming.dispatchGeneration or '') then
		return -2
	end
	if (existing.action or '') == 'STOP' and (incoming.action or '') == 'PAUSE' then
		return 2
	end
	if (existing.action or '') == 'PAUSE' and (incoming.action or '') == 'STOP' then
		redis.call('SET', KEYS[1], incomingRaw, 'EX', ttl)
		redis.call('PUBLISH', KEYS[1], incomingRaw)
		return 1
	end
	return -2
`)

// PublishTaskControl atomically persists and publishes one exact envelope.
// Same-envelope retries republish without renewing TTL; same-generation STOP
// replaces PAUSE, while PAUSE can never overwrite STOP.
func (s *Scheduler) PublishTaskControl(ctx context.Context, envelope TaskControlEnvelope) error {
	data, err := marshalTaskControlEnvelope(envelope)
	if err != nil {
		return err
	}
	key, err := envelope.Key()
	if err != nil {
		return err
	}
	code, err := publishTaskControlScript.Run(ctx, s.rdb, []string{key}, data, int(taskControlTTL/time.Second)).Int64()
	if err != nil {
		return err
	}
	switch code {
	case 0, 1:
		return nil
	case 2:
		return ErrTaskControlSuperseded
	case -4:
		return fmt.Errorf("Redis rejected malformed task control envelope")
	case -3:
		return fmt.Errorf("existing task control value is malformed")
	default:
		return fmt.Errorf("task control key contains a conflicting envelope")
	}
}

func (s *Scheduler) GetTaskControl(ctx context.Context, target TaskControlTarget) (*TaskControlEnvelope, error) {
	if err := target.Validate(); err != nil {
		return nil, err
	}
	key, _ := TaskControlKey(target.TaskID, target.DispatchGeneration)
	data, err := s.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	envelope, err := ParseTaskControlEnvelope(data)
	if err != nil {
		return nil, err
	}
	if envelope.TaskID != target.TaskID || envelope.DispatchGeneration != target.DispatchGeneration {
		return nil, fmt.Errorf("task control key and envelope identity do not match")
	}
	return envelope, nil
}

var deleteExactTaskControlScript = redis.NewScript(`
	if redis.call('GET', KEYS[1]) == ARGV[1] then
		return redis.call('DEL', KEYS[1])
	end
	return 0
`)

// DeleteTaskControlExact removes only the byte-exact envelope supplied by the
// caller. A racing STOP or any other intent/generation remains untouched.
func (s *Scheduler) DeleteTaskControlExact(ctx context.Context, envelope TaskControlEnvelope) (bool, error) {
	data, err := marshalTaskControlEnvelope(envelope)
	if err != nil {
		return false, err
	}
	key, err := envelope.Key()
	if err != nil {
		return false, err
	}
	removed, err := deleteExactTaskControlScript.Run(ctx, s.rdb, []string{key}, data).Int64()
	return removed == 1, err
}

// SubscribeTaskControls accepts only strict JSON envelopes whose exact key is
// also their Pub/Sub channel. Malformed or generation-blind values are ignored.
func (s *Scheduler) SubscribeTaskControls(ctx context.Context) <-chan *TaskControlEnvelope {
	ch := make(chan *TaskControlEnvelope, 100)
	go func() {
		defer close(ch)
		const maxBackoff = 30 * time.Second
		backoff := time.Second
		for {
			if ctx.Err() != nil {
				return
			}
			pubsub := s.rdb.PSubscribe(ctx, TaskControlChannelPattern())
			var closeOnce sync.Once
			closePubsub := func() { closeOnce.Do(func() { _ = pubsub.Close() }) }
			msgCh := pubsub.Channel()
			if _, err := pubsub.Receive(ctx); err != nil {
				logx.Errorf("[Scheduler] Subscribe task controls failed: %v, retry in %v", err, backoff)
				closePubsub()
				sleepTaskControlContext(ctx, backoff)
				backoff = nextTaskControlBackoff(backoff, maxBackoff)
				continue
			}
			backoff = time.Second
			logx.Infof("[Scheduler] Subscribed to %s", TaskControlChannelPattern())

		consumeLoop:
			for {
				select {
				case <-ctx.Done():
					closePubsub()
					return
				case msg, ok := <-msgCh:
					if !ok || msg == nil {
						closePubsub()
						break consumeLoop
					}
					envelope, err := ParseTaskControlEnvelope([]byte(msg.Payload))
					if err != nil {
						logx.Errorf("[Scheduler] Rejected malformed task control on %s: %v", msg.Channel, err)
						continue
					}
					expectedChannel, _ := envelope.Key()
					if msg.Channel != expectedChannel {
						logx.Errorf("[Scheduler] Rejected task control channel mismatch: channel=%s expected=%s", msg.Channel, expectedChannel)
						continue
					}
					select {
					case ch <- envelope:
					default:
						logx.Errorf("[Scheduler] Dropped task control because the local subscription buffer is full: task=%s generation=%s", envelope.TaskID, envelope.DispatchGeneration)
					}
				}
			}
			sleepTaskControlContext(ctx, backoff)
			backoff = nextTaskControlBackoff(backoff, maxBackoff)
		}
	}()
	return ch
}

func sleepTaskControlContext(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func nextTaskControlBackoff(current, maximum time.Duration) time.Duration {
	next := current * 2
	if next > maximum {
		return maximum
	}
	return next
}
