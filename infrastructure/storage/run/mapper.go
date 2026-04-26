package run

import domainrun "github.com/shinya/shineflow/domain/run"

func toRun(m *runModel) *domainrun.WorkflowRun {
	return &domainrun.WorkflowRun{
		ID:             m.ID,
		DefinitionID:   m.DefinitionID,
		VersionID:      m.VersionID,
		TriggerKind:    domainrun.TriggerKind(m.TriggerKind),
		TriggerRef:     m.TriggerRef,
		TriggerPayload: m.TriggerPayload,
		Status:         domainrun.RunStatus(m.Status),
		StartedAt:      m.StartedAt,
		EndedAt:        m.EndedAt,
		Vars:           m.Vars,
		EndNodeID:      m.EndNodeID,
		Output:         m.Output,
		Error:          m.Error.inner,
		CreatedBy:      m.CreatedBy,
		CreatedAt:      m.CreatedAt,
	}
}

func toRunModel(r *domainrun.WorkflowRun) *runModel {
	return &runModel{
		ID:             r.ID,
		DefinitionID:   r.DefinitionID,
		VersionID:      r.VersionID,
		TriggerKind:    string(r.TriggerKind),
		TriggerRef:     r.TriggerRef,
		TriggerPayload: r.TriggerPayload,
		Status:         string(r.Status),
		StartedAt:      r.StartedAt,
		EndedAt:        r.EndedAt,
		Vars:           r.Vars,
		EndNodeID:      r.EndNodeID,
		Output:         r.Output,
		Error:          runErrorColumn{inner: r.Error},
		CreatedBy:      r.CreatedBy,
		CreatedAt:      r.CreatedAt,
	}
}

func toNodeRun(m *nodeRunModel) *domainrun.NodeRun {
	return &domainrun.NodeRun{
		ID:              m.ID,
		RunID:           m.RunID,
		NodeID:          m.NodeID,
		NodeTypeKey:     m.NodeTypeKey,
		Attempt:         m.Attempt,
		Status:          domainrun.NodeRunStatus(m.Status),
		StartedAt:       m.StartedAt,
		EndedAt:         m.EndedAt,
		ResolvedConfig:  m.ResolvedConfig,
		ResolvedInputs:  m.ResolvedInputs,
		Output:          m.Output,
		FiredPort:       m.FiredPort,
		FallbackApplied: m.FallbackApplied,
		Error:           m.Error.inner,
		ExternalRefs:    []domainrun.ExternalRef(m.ExternalRefs),
	}
}

func toNodeRunModel(n *domainrun.NodeRun) *nodeRunModel {
	return &nodeRunModel{
		ID:              n.ID,
		RunID:           n.RunID,
		NodeID:          n.NodeID,
		NodeTypeKey:     n.NodeTypeKey,
		Attempt:         n.Attempt,
		Status:          string(n.Status),
		StartedAt:       n.StartedAt,
		EndedAt:         n.EndedAt,
		ResolvedConfig:  n.ResolvedConfig,
		ResolvedInputs:  n.ResolvedInputs,
		Output:          n.Output,
		FiredPort:       n.FiredPort,
		FallbackApplied: n.FallbackApplied,
		Error:           nodeErrorColumn{inner: n.Error},
		ExternalRefs:    externalRefsColumn(n.ExternalRefs),
	}
}
