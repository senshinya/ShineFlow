package engine

import (
	"context"

	"github.com/shinya/shineflow/domain/run"
)

func (e *Engine) finalize(ctx context.Context, rn *run.WorkflowRun, st *runState) (*run.WorkflowRun, error) {
	switch {
	case st.runFail != nil:
		return e.finalizeFailed(ctx, rn, *st.runFail)
	case st.cancelled:
		return e.finalizeCancelled(ctx, rn)
	case st.endHit != nil:
		return e.finalizeSuccess(ctx, rn, st)
	default:
		return e.finalizeFailed(ctx, rn, run.RunError{
			Code:    run.RunErrCodeNoEndReached,
			Message: "all branches exhausted but no End was reached",
		})
	}
}

func (e *Engine) finalizeSuccess(ctx context.Context, rn *run.WorkflowRun, st *runState) (*run.WorkflowRun, error) {
	ctx = context.WithoutCancel(ctx)
	endID := *st.endHit
	endNR, err := e.runRepo.GetLatestNodeRun(ctx, rn.ID, endID)
	if err != nil {
		return nil, err
	}
	output := defaultJSON(endNR.ResolvedInputs)
	if err := e.runRepo.SaveEndResult(ctx, rn.ID, endID, output); err != nil {
		return nil, err
	}
	endedAt := e.cfg.Clock()
	if err := e.runRepo.UpdateStatus(ctx, rn.ID, run.RunStatusSuccess, run.WithRunEndedAt(endedAt)); err != nil {
		return nil, err
	}
	rn.Status = run.RunStatusSuccess
	rn.EndNodeID = &endID
	rn.Output = output
	rn.EndedAt = &endedAt
	return rn, nil
}

func (e *Engine) finalizeFailed(ctx context.Context, rn *run.WorkflowRun, runErr run.RunError) (*run.WorkflowRun, error) {
	ctx = context.WithoutCancel(ctx)
	if err := e.runRepo.SaveError(ctx, rn.ID, runErr); err != nil {
		return nil, err
	}
	endedAt := e.cfg.Clock()
	if err := e.runRepo.UpdateStatus(ctx, rn.ID, run.RunStatusFailed, run.WithRunEndedAt(endedAt)); err != nil {
		return nil, err
	}
	rn.Status = run.RunStatusFailed
	rn.Error = &runErr
	rn.EndedAt = &endedAt
	return rn, nil
}

func (e *Engine) finalizeCancelled(ctx context.Context, rn *run.WorkflowRun) (*run.WorkflowRun, error) {
	ctx = context.WithoutCancel(ctx)
	endedAt := e.cfg.Clock()
	if err := e.runRepo.UpdateStatus(ctx, rn.ID, run.RunStatusCancelled, run.WithRunEndedAt(endedAt)); err != nil {
		return nil, err
	}
	rn.Status = run.RunStatusCancelled
	rn.EndedAt = &endedAt
	return rn, nil
}
