package engine

import (
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

// Resolver 基于 Symbols 快照解析节点输入和值模板。
type Resolver struct {
	dsl  *workflow.WorkflowDSL
	mode TemplateMode
}

func newResolver(dsl *workflow.WorkflowDSL, mode TemplateMode) *Resolver {
	return &Resolver{dsl: dsl, mode: mode}
}

// ResolveInputs 解析节点 Inputs 中的每个 ValueSource。
func (r *Resolver) ResolveInputs(node *workflow.Node, sym *run.Symbols) (map[string]any, error) {
	if len(node.Inputs) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(node.Inputs))
	for k, vs := range node.Inputs {
		v, err := r.resolveOne(vs, sym)
		if err != nil {
			return nil, fmt.Errorf("at inputs.%s: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

// ResolveConfig 递归展开 Config 中所有字符串叶子的模板。
func (r *Resolver) ResolveConfig(raw json.RawMessage, sym *run.Symbols) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	expanded, err := walkExpand(root, sym, r.mode)
	if err != nil {
		return nil, err
	}
	m, ok := expanded.(map[string]any)
	if !ok {
		return map[string]any{"_root": expanded}, nil
	}
	return m, nil
}

func (r *Resolver) resolveOne(vs workflow.ValueSource, sym *run.Symbols) (any, error) {
	switch vs.Kind {
	case workflow.ValueKindLiteral:
		return vs.Value, nil
	case workflow.ValueKindRef:
		ref, ok := vs.Value.(workflow.RefValue)
		if !ok {
			ref = coerceRefValue(vs.Value)
		}
		return r.resolveRef(ref, sym)
	case workflow.ValueKindTemplate:
		s, ok := vs.Value.(string)
		if !ok {
			return nil, fmt.Errorf("template value must be string, got %T", vs.Value)
		}
		return expandTemplate(s, sym, r.mode)
	default:
		return nil, fmt.Errorf("unknown ValueSource kind: %v", vs.Kind)
	}
}

func (r *Resolver) resolveRef(ref workflow.RefValue, sym *run.Symbols) (any, error) {
	if ref.NodeID == "" {
		return nil, fmt.Errorf("ref node_id is required")
	}
	if r.dsl != nil {
		found := false
		for i := range r.dsl.Nodes {
			if r.dsl.Nodes[i].ID == ref.NodeID {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("ref node not found in DSL: %s", ref.NodeID)
		}
	}
	full := "nodes." + ref.NodeID
	if ref.Path != "" {
		full += "." + ref.Path
	}
	return sym.Lookup(full)
}

func coerceRefValue(v any) workflow.RefValue {
	m, _ := v.(map[string]any)
	out := workflow.RefValue{}
	if s, ok := m["node_id"].(string); ok {
		out.NodeID = s
	}
	if s, ok := m["path"].(string); ok {
		out.Path = s
	}
	if s, ok := m["name"].(string); ok {
		out.Name = s
	}
	return out
}

func walkExpand(v any, sym *run.Symbols, mode TemplateMode) (any, error) {
	switch x := v.(type) {
	case string:
		return expandTemplate(x, sym, mode)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			ev, err := walkExpand(val, sym, mode)
			if err != nil {
				return nil, fmt.Errorf("at %s: %w", k, err)
			}
			out[k] = ev
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			ev, err := walkExpand(val, sym, mode)
			if err != nil {
				return nil, fmt.Errorf("at [%d]: %w", i, err)
			}
			out[i] = ev
		}
		return out, nil
	default:
		return v, nil
	}
}
