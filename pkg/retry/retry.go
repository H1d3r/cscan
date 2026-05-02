package retry

import (
	"context"
	"fmt"
	"time"

	"cscan/pkg/xerr"
)

// Config 重试配置
type Config struct {
	MaxRetries     int           // 最大重试次数
	InitialBackoff time.Duration // 初始退避时间
	MaxBackoff     time.Duration // 最大退避时间
	Multiplier     float64       // 退避时间乘数
}

// DefaultConfig 默认重试配置
var DefaultConfig = Config{
	MaxRetries:     3,
	InitialBackoff: 1 * time.Second,
	MaxBackoff:     30 * time.Second,
	Multiplier:     2.0,
}

// Operation 可重试的操作函数类型
type Operation func() error

// Do 执行带重试的操作
// 使用默认配置
func Do(ctx context.Context, op Operation) error {
	return DoWithConfig(ctx, DefaultConfig, op)
}

// DoWithConfig 使用指定配置执行带重试的操作
func DoWithConfig(ctx context.Context, config Config, op Operation) error {
	var lastErr error
	backoff := config.InitialBackoff

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// 检查 context 是否已取消
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("context canceled after %d attempts: %w", attempt, lastErr)
			}
			return ctx.Err()
		default:
		}

		// 执行操作
		err := op()
		if err == nil {
			return nil
		}

		lastErr = err

		// 检查错误是否可重试
		if !xerr.IsRetryable(err) {
			return err
		}

		// 如果还有重试机会，等待后重试
		if attempt < config.MaxRetries {
			select {
			case <-ctx.Done():
				return fmt.Errorf("context canceled during backoff: %w", lastErr)
			case <-time.After(backoff):
			}

			// 计算下一次退避时间
			backoff = time.Duration(float64(backoff) * config.Multiplier)
			if backoff > config.MaxBackoff {
				backoff = config.MaxBackoff
			}
		}
	}

	return fmt.Errorf("max retries (%d) exceeded: %w", config.MaxRetries, lastErr)
}

// WithMaxRetries 创建指定最大重试次数的配置
func WithMaxRetries(maxRetries int) Config {
	config := DefaultConfig
	config.MaxRetries = maxRetries
	return config
}

// WithBackoff 创建指定退避参数的配置
func WithBackoff(initial, max time.Duration, multiplier float64) Config {
	config := DefaultConfig
	config.InitialBackoff = initial
	config.MaxBackoff = max
	config.Multiplier = multiplier
	return config
}

// NewConfig 创建自定义配置
func NewConfig(maxRetries int, initialBackoff, maxBackoff time.Duration, multiplier float64) Config {
	return Config{
		MaxRetries:     maxRetries,
		InitialBackoff: initialBackoff,
		MaxBackoff:     maxBackoff,
		Multiplier:     multiplier,
	}
}

// Result 重试结果，包含尝试次数信息
type Result struct {
	Attempts int   // 实际尝试次数
	Err      error // 最终错误（如果有）
}

// DoWithResult 执行带重试的操作并返回详细结果
func DoWithResult(ctx context.Context, config Config, op Operation) Result {
	var lastErr error
	backoff := config.InitialBackoff
	attempts := 0

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		attempts = attempt + 1

		// 检查 context 是否已取消
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return Result{
					Attempts: attempts,
					Err:      fmt.Errorf("context canceled after %d attempts: %w", attempts, lastErr),
				}
			}
			return Result{Attempts: attempts, Err: ctx.Err()}
		default:
		}

		// 执行操作
		err := op()
		if err == nil {
			return Result{Attempts: attempts, Err: nil}
		}

		lastErr = err

		// 检查错误是否可重试
		if !xerr.IsRetryable(err) {
			return Result{Attempts: attempts, Err: err}
		}

		// 如果还有重试机会，等待后重试
		if attempt < config.MaxRetries {
			select {
			case <-ctx.Done():
				return Result{
					Attempts: attempts,
					Err:      fmt.Errorf("context canceled during backoff: %w", lastErr),
				}
			case <-time.After(backoff):
			}

			// 计算下一次退避时间
			backoff = time.Duration(float64(backoff) * config.Multiplier)
			if backoff > config.MaxBackoff {
				backoff = config.MaxBackoff
			}
		}
	}

	return Result{
		Attempts: attempts,
		Err:      fmt.Errorf("max retries (%d) exceeded: %w", config.MaxRetries, lastErr),
	}
}
