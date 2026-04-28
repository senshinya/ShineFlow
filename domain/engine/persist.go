package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shinya/shineflow/domain/run"
)

type persistKind int

const (
	persistAppendNodeRun persistKind = iota
	persistNodeRunSuccess
	persistNodeRunFailed
	persistNodeRunCancelled
	persistNodeRunFallbackPatch
	persistRetryAborted
	persistSaveVars
	persistMarkSkipped
)

// persistOp 是 driver 发给 persister 的值消息。
type persistOp struct {
	kind    persistKind
	runID   string
	payload any
}

// runPersister 顺序消费持久化消息，首个错误通过 errOut 报告，同时继续尽力 drain。
func (e *Engine) runPersister(ctx context.Context, in <-chan persistOp, errOut chan<- error, doneOut chan<- struct{}) {
	defer close(doneOut)
	for op := range in {
		if err := e.applyPersistOp(ctx, op); err != nil {
			select {
			case errOut <- err:
			default:
			}
		}
	}
}

func (e *Engine) applyPersistOp(ctx context.Context, op persistOp) error {
	switch op.kind {
	case persistAppendNodeRun:
		nr, ok := op.payload.(*run.NodeRun)
		if !ok || nr == nil {
			return fmt.Errorf("persist append node run: invalid payload")
		}
		return e.runRepo.AppendNodeRun(ctx, op.runID, nr)
	case persistNodeRunSuccess:
		return e.applyAttemptFinish(ctx, op, run.NodeRunStatusSuccess)
	case persistNodeRunFailed:
		return e.applyAttemptFinish(ctx, op, run.NodeRunStatusFailed)
	case persistNodeRunCancelled:
		return e.applyAttemptFinish(ctx, op, run.NodeRunStatusCancelled)
	case persistNodeRunFallbackPatch:
		res, ok := op.payload.(nodeResult)
		if !ok {
			return fmt.Errorf("persist fallback patch: invalid payload")
		}
		if err := e.saveResolved(ctx, op.runID, res); err != nil {
			return err
		}
		outRaw, err := marshalOutput(res.output)
		if err != nil {
			return fmt.Errorf("marshal fallback output: %w", err)
		}
		if err := e.runRepo.SaveNodeRunOutput(ctx, op.runID, res.nodeRunID, outRaw, res.firedPort); err != nil {
			return err
		}
		opts := []run.NodeRunUpdateOpt{
			run.WithNodeRunFallbackApplied(true),
			run.WithNodeRunEndedAt(e.cfg.Clock()),
		}
		if res.err != nil {
			opts = append(opts, run.WithNodeRunError(run.NodeError{
				Code:    run.NodeErrCodeExecFailed,
				Message: res.err.Error(),
			}))
		}
		return e.runRepo.UpdateNodeRunStatus(ctx, op.runID, res.nodeRunID, run.NodeRunStatusFailed, opts...)
	case persistRetryAborted:
		nr, ok := op.payload.(*run.NodeRun)
		if !ok || nr == nil {
			return fmt.Errorf("persist retry aborted: invalid payload")
		}
		return e.runRepo.AppendNodeRun(ctx, op.runID, nr)
	case persistSaveVars:
		raw, ok := op.payload.(json.RawMessage)
		if !ok {
			return fmt.Errorf("persist vars: invalid payload")
		}
		return e.runRepo.SaveVars(ctx, op.runID, raw)
	case persistMarkSkipped:
		nr, ok := op.payload.(*run.NodeRun)
		if !ok || nr == nil {
			return fmt.Errorf("persist skipped: invalid payload")
		}
		return e.runRepo.AppendNodeRun(ctx, op.runID, nr)
	}
	return fmt.Errorf("unknown persistKind: %d", op.kind)
}

func (e *Engine) applyAttemptFinish(ctx context.Context, op persistOp, status run.NodeRunStatus) error {
	res, ok := op.payload.(nodeResult)
	if !ok {
		return fmt.Errorf("persist attempt finish: invalid payload")
	}
	if err := e.saveResolved(ctx, op.runID, res); err != nil {
		return err
	}
	if status == run.NodeRunStatusSuccess || res.firedPort != "" || res.output != nil {
		outRaw, err := marshalOutput(res.output)
		if err != nil {
			return fmt.Errorf("marshal node output: %w", err)
		}
		if err := e.runRepo.SaveNodeRunOutput(ctx, op.runID, res.nodeRunID, outRaw, res.firedPort); err != nil {
			return err
		}
	}
	opts := []run.NodeRunUpdateOpt{run.WithNodeRunEndedAt(e.cfg.Clock())}
	if status == run.NodeRunStatusSuccess && len(res.externalRefs) > 0 {
		opts = append(opts, run.WithNodeRunExternalRefs(res.externalRefs))
	}
	if res.err != nil {
		opts = append(opts, run.WithNodeRunError(nodeErrorFor(res.err, status)))
	}
	return e.runRepo.UpdateNodeRunStatus(ctx, op.runID, res.nodeRunID, status, opts...)
}

func (e *Engine) saveResolved(ctx context.Context, runID string, res nodeResult) error {
	if len(res.resolvedConfig) == 0 && len(res.resolvedInputs) == 0 {
		return nil
	}
	return e.runRepo.SaveNodeRunResolved(ctx, runID, res.nodeRunID, defaultJSON(res.resolvedConfig), defaultJSON(res.resolvedInputs))
}

func nodeErrorFor(err error, status run.NodeRunStatus) run.NodeError {
	code := run.NodeErrCodeExecFailed
	if status == run.NodeRunStatusCancelled {
		code = run.NodeErrCodeCancelled
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = run.NodeErrCodeTimeout
	}
	return run.NodeError{Code: code, Message: err.Error()}
}

func marshalOutput(out map[string]any) (json.RawMessage, error) {
	if out == nil {
		return json.RawMessage(`null`), nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func defaultJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}
