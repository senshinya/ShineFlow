package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

func (e *Engine) runNode(
	ctx context.Context,
	rn *run.WorkflowRun,
	node *workflow.Node,
	nr *run.NodeRun,
	snap *run.Symbols,
	resolver *Resolver,
	done chan<- nodeResult,
) {
	res := nodeResult{nodeID: node.ID, nodeRunID: nr.ID, attempt: nr.Attempt}
	defer func() {
		if r := recover(); r != nil {
			res.err = fmt.Errorf("executor panic: %v", r)
		}
		done <- res
	}()

	inputs, err := resolver.ResolveInputs(node, snap)
	if err != nil {
		res.err = fmt.Errorf("resolve inputs: %w", err)
		return
	}
	resolvedConfig, err := resolver.ResolveConfig(node.Config, snap)
	if err != nil {
		res.err = fmt.Errorf("resolve config: %w", err)
		return
	}
	resolvedInputsRaw, err := json.Marshal(inputs)
	if err != nil {
		res.err = fmt.Errorf("marshal resolved inputs: %w", err)
		return
	}
	resolvedConfigRaw, err := json.Marshal(resolvedConfig)
	if err != nil {
		res.err = fmt.Errorf("marshal resolved config: %w", err)
		return
	}
	res.resolvedInputs = resolvedInputsRaw
	res.resolvedConfig = resolvedConfigRaw

	nt, ok := e.ntReg.Get(node.TypeKey)
	if !ok {
		res.err = fmt.Errorf("node type not registered: %s", node.TypeKey)
		return
	}
	exe, err := e.exReg.Build(nt)
	if err != nil {
		res.err = fmt.Errorf("executor build: %w", err)
		return
	}

	nodeCtx := ctx
	if timeout := effectivePolicy(node.ErrorPolicy).Timeout; timeout > 0 {
		var cancel context.CancelFunc
		nodeCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	out, err := exe.Execute(nodeCtx, executor.ExecInput{
		NodeType: nt,
		Config:   resolvedConfigRaw,
		Inputs:   inputs,
		Run:      buildRunInfo(rn, nr),
		Services: e.services,
	})
	if err != nil {
		res.err = err
		return
	}
	res.output = out.Outputs
	res.firedPort = out.FiredPort
	if res.firedPort == "" {
		res.firedPort = workflow.PortDefault
	}
	res.externalRefs = out.ExternalRefs
}

func buildRunInfo(rn *run.WorkflowRun, nr *run.NodeRun) executor.RunInfo {
	return executor.RunInfo{
		RunID:          rn.ID,
		NodeRunID:      nr.ID,
		Attempt:        nr.Attempt,
		DefinitionID:   rn.DefinitionID,
		VersionID:      rn.VersionID,
		TriggerKind:    rn.TriggerKind,
		TriggerRef:     rn.TriggerRef,
		TriggerPayload: rn.TriggerPayload,
	}
}
