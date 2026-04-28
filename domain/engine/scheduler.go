package engine

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

func buildTriggerTable(dsl workflow.WorkflowDSL) (triggerTable, outAdj) {
	tt := triggerTable{}
	oa := outAdj{}
	for i := range dsl.Nodes {
		n := &dsl.Nodes[i]
		spec := &triggerSpec{nodeID: n.ID}
		if n.TypeKey == nodetype.BuiltinJoin {
			spec.mode = parseJoinMode(n.Config)
		}
		tt[n.ID] = spec
	}
	for _, e := range dsl.Edges {
		if t, ok := tt[e.To]; ok {
			t.inEdges = append(t.inEdges, inEdgeRef{
				EdgeID:     e.ID,
				SourceNode: e.From,
				SourcePort: e.FromPort,
			})
		}
		oa[e.From] = append(oa[e.From], e)
	}
	return tt, oa
}

func parseJoinMode(raw json.RawMessage) joinMode {
	var cfg struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(raw, &cfg)
	if cfg.Mode == nodetype.JoinModeAll {
		return joinAll
	}
	return joinAny
}

func evaluate(spec *triggerSpec, es map[string]edgeState) readiness {
	if spec == nil {
		return notReady
	}
	n := len(spec.inEdges)
	if n == 0 {
		return readyToRun
	}
	hasLive, hasPending, hasDead := false, false, false
	for _, e := range spec.inEdges {
		switch es[e.EdgeID] {
		case edgePending:
			hasPending = true
		case edgeLive:
			hasLive = true
		case edgeDead:
			hasDead = true
		}
	}
	if n == 1 {
		if hasPending {
			return notReady
		}
		if hasLive {
			return readyToRun
		}
		return readyToSkip
	}
	switch spec.mode {
	case joinAny:
		if hasLive {
			return readyToRun
		}
		if hasPending {
			return notReady
		}
		return readyToSkip
	case joinAll:
		if hasPending {
			return notReady
		}
		if hasDead {
			return readyToSkip
		}
		return readyToRun
	default:
		return notReady
	}
}

// dispatch 创建 NodeRun、排入持久化队列，并启动 worker。
func (e *Engine) dispatch(
	ctx context.Context,
	rn *run.WorkflowRun,
	st *runState,
	nodeID string,
	done chan<- nodeResult,
	persistCh chan<- persistOp,
) {
	node := st.byID[nodeID]
	attempt := st.attemptCounter[nodeID] + 1
	st.attemptCounter[nodeID] = attempt

	startedAt := e.cfg.Clock()
	nr := &run.NodeRun{
		ID:          e.cfg.NewID(),
		RunID:       rn.ID,
		NodeID:      nodeID,
		NodeTypeKey: node.TypeKey,
		Attempt:     attempt,
		Status:      run.NodeRunStatusRunning,
		StartedAt:   &startedAt,
	}
	persistCh <- persistOp{kind: persistAppendNodeRun, runID: rn.ID, payload: nr}

	st.nodeStat[nodeID] = nodeRunning
	st.inflight++

	snap := st.sym.Snapshot()
	resolver := newResolver(&st.dsl, e.cfg.TemplateMode)
	go e.runNode(ctx, rn, node, nr, snap, resolver, done)
}

func (e *Engine) markSkipped(rn *run.WorkflowRun, st *runState, nodeID string, persistCh chan<- persistOp) {
	if st.nodeStat[nodeID] == nodeDone {
		return
	}
	st.nodeStat[nodeID] = nodeDone
	endedAt := e.cfg.Clock()
	nr := &run.NodeRun{
		ID:          e.cfg.NewID(),
		RunID:       rn.ID,
		NodeID:      nodeID,
		NodeTypeKey: st.byID[nodeID].TypeKey,
		Attempt:     1,
		Status:      run.NodeRunStatusSkipped,
		EndedAt:     &endedAt,
	}
	persistCh <- persistOp{kind: persistMarkSkipped, runID: rn.ID, payload: nr}
}

func (e *Engine) persistRetryAborted(rn *run.WorkflowRun, st *runState, rt retryEvent, persistCh chan<- persistOp) {
	endedAt := e.cfg.Clock()
	nr := &run.NodeRun{
		ID:          e.cfg.NewID(),
		RunID:       rn.ID,
		NodeID:      rt.nodeID,
		NodeTypeKey: st.byID[rt.nodeID].TypeKey,
		Attempt:     rt.attempt,
		Status:      run.NodeRunStatusCancelled,
		StartedAt:   &endedAt,
		EndedAt:     &endedAt,
		Error: &run.NodeError{
			Code:    run.NodeErrCodeCancelled,
			Message: "ctx cancelled before retry timer fired",
		},
	}
	persistCh <- persistOp{kind: persistRetryAborted, runID: rn.ID, payload: nr}
}

func (e *Engine) handleResult(
	ctx context.Context,
	rn *run.WorkflowRun,
	st *runState,
	res nodeResult,
	done chan<- nodeResult,
	retryCh chan<- retryEvent,
	persistCh chan<- persistOp,
) {
	if st.cancelled || (st.endHit == nil && st.runFail == nil && ctx.Err() != nil) {
		st.cancelled = true
		persistCh <- persistOp{kind: persistNodeRunCancelled, runID: rn.ID, payload: res}
		st.nodeStat[res.nodeID] = nodeDone
		return
	}
	if st.endHit != nil || st.runFail != nil {
		persistCh <- persistOp{kind: persistNodeRunCancelled, runID: rn.ID, payload: res}
		st.nodeStat[res.nodeID] = nodeDone
		return
	}

	node := st.byID[res.nodeID]
	ep := effectivePolicy(node.ErrorPolicy)

	if res.err != nil {
		if ctx.Err() != nil && (errors.Is(res.err, context.Canceled) || errors.Is(res.err, context.DeadlineExceeded)) {
			persistCh <- persistOp{kind: persistNodeRunCancelled, runID: rn.ID, payload: res}
			st.nodeStat[res.nodeID] = nodeDone
			st.cancelled = true
			return
		}

		if res.attempt < ep.MaxRetries+1 {
			persistCh <- persistOp{kind: persistNodeRunFailed, runID: rn.ID, payload: res}
			delay := computeBackoff(ep, res.attempt, st.rng)
			st.pendingRetries++
			e.scheduleRetry(ctx, res.nodeID, res.attempt+1, delay, retryCh)
			return
		}

		switch ep.OnFinalFail {
		case workflow.FailStrategyFallback:
			res.output = ep.FallbackOutput.Output
			res.firedPort = ep.FallbackOutput.Port
			if res.firedPort == "" {
				res.firedPort = workflow.PortDefault
			}
			res.fallbackApplied = true
		case workflow.FailStrategyFireErrorPort:
			res.output = map[string]any{"error": map[string]any{"message": res.err.Error()}}
			res.firedPort = workflow.PortError
		case workflow.FailStrategyFailRun:
			persistCh <- persistOp{kind: persistNodeRunFailed, runID: rn.ID, payload: res}
			st.runFail = &run.RunError{
				NodeID:    res.nodeID,
				NodeRunID: res.nodeRunID,
				Code:      run.RunErrCodeNodeExecFailed,
				Message:   res.err.Error(),
			}
			st.nodeStat[res.nodeID] = nodeDone
			return
		default:
			persistCh <- persistOp{kind: persistNodeRunFailed, runID: rn.ID, payload: res}
			st.runFail = &run.RunError{
				NodeID:    res.nodeID,
				NodeRunID: res.nodeRunID,
				Code:      run.RunErrCodeNodeExecFailed,
				Message:   res.err.Error(),
			}
			st.nodeStat[res.nodeID] = nodeDone
			return
		}
	}

	e.propagate(ctx, rn, st, node, res, done, persistCh)
}

func (e *Engine) propagate(
	ctx context.Context,
	rn *run.WorkflowRun,
	st *runState,
	node *workflow.Node,
	res nodeResult,
	done chan<- nodeResult,
	persistCh chan<- persistOp,
) {
	if res.output == nil {
		res.output = map[string]any{}
	}
	if err := st.sym.SetNodeOutput(node.ID, res.output); err != nil {
		res.err = err
		res.output = nil
		res.firedPort = ""
		persistCh <- persistOp{kind: persistNodeRunFailed, runID: rn.ID, payload: res}
		st.runFail = &run.RunError{
			NodeID:    res.nodeID,
			NodeRunID: res.nodeRunID,
			Code:      run.RunErrCodeOutputNotSerializable,
			Message:   err.Error(),
		}
		st.nodeStat[res.nodeID] = nodeDone
		return
	}
	var varsRaw json.RawMessage
	if node.TypeKey == nodetype.BuiltinSetVariable {
		for k, v := range res.output {
			if err := st.sym.SetVar(k, v); err != nil {
				res.err = err
				res.output = nil
				res.firedPort = ""
				persistCh <- persistOp{kind: persistNodeRunFailed, runID: rn.ID, payload: res}
				st.runFail = &run.RunError{
					NodeID:    res.nodeID,
					NodeRunID: res.nodeRunID,
					Code:      run.RunErrCodeOutputNotSerializable,
					Message:   err.Error(),
				}
				st.nodeStat[res.nodeID] = nodeDone
				return
			}
		}
		var err error
		varsRaw, err = json.Marshal(st.sym.SnapshotVars())
		if err != nil {
			res.err = err
			res.output = nil
			res.firedPort = ""
			persistCh <- persistOp{kind: persistNodeRunFailed, runID: rn.ID, payload: res}
			st.runFail = &run.RunError{NodeID: res.nodeID, NodeRunID: res.nodeRunID, Code: run.RunErrCodeOutputNotSerializable, Message: err.Error()}
			st.nodeStat[res.nodeID] = nodeDone
			return
		}
	}
	persistResult(rn, res, persistCh)
	if varsRaw != nil {
		persistCh <- persistOp{kind: persistSaveVars, runID: rn.ID, payload: varsRaw}
	}
	st.nodeStat[node.ID] = nodeDone

	if node.TypeKey == nodetype.BuiltinEnd {
		if st.endHit == nil {
			id := node.ID
			st.endHit = &id
		}
		return
	}

	for _, edge := range st.outAdj[node.ID] {
		if edge.FromPort == res.firedPort {
			st.edgeState[edge.ID] = edgeLive
		} else {
			st.edgeState[edge.ID] = edgeDead
		}
	}
	for _, edge := range st.outAdj[node.ID] {
		e.tryAdvance(ctx, rn, st, edge.To, done, persistCh)
	}
}

func persistResult(rn *run.WorkflowRun, res nodeResult, persistCh chan<- persistOp) {
	switch {
	case res.fallbackApplied:
		persistCh <- persistOp{kind: persistNodeRunFallbackPatch, runID: rn.ID, payload: res}
	case res.err != nil:
		persistCh <- persistOp{kind: persistNodeRunFailed, runID: rn.ID, payload: res}
	default:
		persistCh <- persistOp{kind: persistNodeRunSuccess, runID: rn.ID, payload: res}
	}
}

func (e *Engine) tryAdvance(
	ctx context.Context,
	rn *run.WorkflowRun,
	st *runState,
	target string,
	done chan<- nodeResult,
	persistCh chan<- persistOp,
) {
	if st.nodeStat[target] != nodeUnready {
		return
	}
	switch evaluate(st.triggers[target], st.edgeState) {
	case readyToRun:
		e.dispatch(ctx, rn, st, target, done, persistCh)
	case readyToSkip:
		e.markSkipped(rn, st, target, persistCh)
		for _, oe := range st.outAdj[target] {
			st.edgeState[oe.ID] = edgeDead
		}
		for _, oe := range st.outAdj[target] {
			e.tryAdvance(ctx, rn, st, oe.To, done, persistCh)
		}
	case notReady:
	}
}
