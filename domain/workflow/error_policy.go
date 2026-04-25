package workflow

import "time"

// BackoffKind 指明 ErrorPolicy.RetryDelay 的退避策略。
type BackoffKind string

const (
	BackoffFixed       BackoffKind = "fixed"
	BackoffExponential BackoffKind = "exponential"
)

// FailStrategy 指明所有重试耗尽后的兜底行为。
type FailStrategy string

const (
	FailStrategyFireErrorPort FailStrategy = "fire_error_port"
	FailStrategyFallback      FailStrategy = "fallback"
	FailStrategyFailRun       FailStrategy = "fail_run"
)

// ErrorPolicy 描述节点的超时 / 重试 / 兜底策略。Node.ErrorPolicy 为 nil 时引擎使用默认策略。
type ErrorPolicy struct {
	Timeout      time.Duration
	MaxRetries   int
	RetryBackoff BackoffKind
	RetryDelay   time.Duration

	OnFinalFail    FailStrategy
	FallbackOutput map[string]any
}
