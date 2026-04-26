// Package validator 实现 WorkflowDSL 的严格校验（PublishVersion 时必过）。
//
// 本包独立于 domain/workflow，避免引入 workflow → nodetype → workflow 的循环依赖。
package validator

import (
	"fmt"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

// ValidationError 描述一条校验违例。Code 是可机读分类，Message 是给用户的可读描述，
// Path 指向出错位置（如 "nodes[2].inputs.in_prompt"），可为空。
type ValidationError struct {
	Code    string
	Message string
	Path    string
}

// 校验违例 Code 常量；与 spec §6.6 的 8 条规则一一对应。
const (
	CodeMissingStart                 = "missing_start"
	CodeMissingEnd                   = "missing_end"
	CodeDuplicateNodeID              = "duplicate_node_id"
	CodeDuplicateEdgeID              = "duplicate_edge_id"
	CodeDuplicatePortID              = "duplicate_port_id"
	CodeDanglingEdge                 = "dangling_edge"
	CodeDanglingRef                  = "dangling_ref"
	CodeUnknownFromPort              = "unknown_from_port"
	CodeRequiredInputMissing         = "required_input_missing"
	CodeUnknownNodeType              = "unknown_node_type"
	CodeFallbackOnNonDefaultPortNode = "fallback_on_non_default_port_node"
	CodeCycle                        = "cycle"
)

// ValidationResult 是 ValidateForPublish 的返回值，一次性收集所有违例。
type ValidationResult struct {
	Errors []ValidationError
}

// OK 是否通过校验。
func (r ValidationResult) OK() bool { return len(r.Errors) == 0 }

// ValidateForPublish 对一个 WorkflowDSL 做 spec §6.6 的全部 8 条严格校验，
// 不在第一条违例时短路，而是收集所有违例后一并返回，便于前端一次提示。
func ValidateForPublish(dsl workflow.WorkflowDSL, reg nodetype.NodeTypeRegistry) ValidationResult {
	var errs []ValidationError

	errs = append(errs, checkStartEnd(dsl)...)
	errs = append(errs, checkUniqueIDs(dsl)...)

	nodeByID := map[string]*workflow.Node{}
	for i := range dsl.Nodes {
		nodeByID[dsl.Nodes[i].ID] = &dsl.Nodes[i]
	}

	typeCache := map[string]*nodetype.NodeType{}
	getType := func(key string) (*nodetype.NodeType, bool) {
		if nt, ok := typeCache[key]; ok {
			return nt, nt != nil
		}
		nt, ok := reg.Get(key)
		typeCache[key] = nt
		if !ok {
			return nil, false
		}
		return nt, true
	}

	errs = append(errs, checkNodeTypesExist(dsl, getType)...)
	errs = append(errs, checkEdgeTargets(dsl, nodeByID)...)
	errs = append(errs, checkEdgeFromPorts(dsl, nodeByID, getType)...)
	errs = append(errs, checkRefValues(dsl, nodeByID, getType)...)
	errs = append(errs, checkRequiredInputs(dsl, getType)...)
	errs = append(errs, checkFallbackOnly(dsl, getType)...)
	errs = append(errs, checkAcyclic(dsl, nodeByID)...)

	return ValidationResult{Errors: errs}
}

// 规则 1：至少 1 个 builtin.start，至少 1 个 builtin.end。
func checkStartEnd(dsl workflow.WorkflowDSL) []ValidationError {
	hasStart, hasEnd := false, false
	for _, n := range dsl.Nodes {
		switch n.TypeKey {
		case nodetype.BuiltinStart:
			hasStart = true
		case nodetype.BuiltinEnd:
			hasEnd = true
		}
	}
	var out []ValidationError
	if !hasStart {
		out = append(out, ValidationError{Code: CodeMissingStart, Message: "DSL must contain at least one builtin.start node"})
	}
	if !hasEnd {
		out = append(out, ValidationError{Code: CodeMissingEnd, Message: "DSL must contain at least one builtin.end node"})
	}
	return out
}

// 规则 2：Node.ID / Edge.ID / 单节点内 PortID 唯一。
func checkUniqueIDs(dsl workflow.WorkflowDSL) []ValidationError {
	var out []ValidationError
	seenN := map[string]int{}
	for i, n := range dsl.Nodes {
		if first, ok := seenN[n.ID]; ok {
			out = append(out, ValidationError{
				Code:    CodeDuplicateNodeID,
				Message: fmt.Sprintf("node id %q used twice (nodes[%d] and nodes[%d])", n.ID, first, i),
				Path:    fmt.Sprintf("nodes[%d].id", i),
			})
		} else {
			seenN[n.ID] = i
		}
	}
	seenE := map[string]int{}
	for i, e := range dsl.Edges {
		if first, ok := seenE[e.ID]; ok {
			out = append(out, ValidationError{
				Code:    CodeDuplicateEdgeID,
				Message: fmt.Sprintf("edge id %q used twice (edges[%d] and edges[%d])", e.ID, first, i),
				Path:    fmt.Sprintf("edges[%d].id", i),
			})
		} else {
			seenE[e.ID] = i
		}
	}
	// PortID 唯一性：在每个 Node.Inputs 的 key 集合内（key 即 PortID）。Inputs 是 map，天然 key 唯一，
	// 这里仅占位规则；如未来 PortSpec 落到 DSL 内还需扩展。
	return out
}

// 规则 6：Node.TypeKey 必须能在 Registry 解析到。
func checkNodeTypesExist(dsl workflow.WorkflowDSL, getType func(string) (*nodetype.NodeType, bool)) []ValidationError {
	var out []ValidationError
	for i, n := range dsl.Nodes {
		if _, ok := getType(n.TypeKey); !ok {
			out = append(out, ValidationError{
				Code:    CodeUnknownNodeType,
				Message: fmt.Sprintf("unknown NodeType %q on node %q", n.TypeKey, n.ID),
				Path:    fmt.Sprintf("nodes[%d].type_key", i),
			})
		}
	}
	return out
}

// 规则 3a：Edge.From / To 必须指向 DSL 内真实节点。
func checkEdgeTargets(dsl workflow.WorkflowDSL, nodeByID map[string]*workflow.Node) []ValidationError {
	var out []ValidationError
	for i, e := range dsl.Edges {
		if _, ok := nodeByID[e.From]; !ok {
			out = append(out, ValidationError{
				Code:    CodeDanglingEdge,
				Message: fmt.Sprintf("edge %q from non-existent node %q", e.ID, e.From),
				Path:    fmt.Sprintf("edges[%d].from", i),
			})
		}
		if _, ok := nodeByID[e.To]; !ok {
			out = append(out, ValidationError{
				Code:    CodeDanglingEdge,
				Message: fmt.Sprintf("edge %q to non-existent node %q", e.ID, e.To),
				Path:    fmt.Sprintf("edges[%d].to", i),
			})
		}
	}
	return out
}

// 规则 4：Edge.FromPort 必须是源节点 NodeType 声明的端口。
func checkEdgeFromPorts(
	dsl workflow.WorkflowDSL,
	nodeByID map[string]*workflow.Node,
	getType func(string) (*nodetype.NodeType, bool),
) []ValidationError {
	var out []ValidationError
	for i, e := range dsl.Edges {
		src, ok := nodeByID[e.From]
		if !ok {
			continue // 已由 checkEdgeTargets 报告
		}
		nt, ok := getType(src.TypeKey)
		if !ok {
			continue // 已由 checkNodeTypesExist 报告
		}
		valid := false
		for _, p := range nt.Ports {
			if p == e.FromPort {
				valid = true
				break
			}
		}
		if !valid {
			out = append(out, ValidationError{
				Code:    CodeUnknownFromPort,
				Message: fmt.Sprintf("edge %q uses port %q not declared by NodeType %q", e.ID, e.FromPort, src.TypeKey),
				Path:    fmt.Sprintf("edges[%d].from_port", i),
			})
		}
	}
	return out
}

// 规则 3b：RefValue.NodeID / PortID 必须真实存在。
func checkRefValues(
	dsl workflow.WorkflowDSL,
	nodeByID map[string]*workflow.Node,
	getType func(string) (*nodetype.NodeType, bool),
) []ValidationError {
	var out []ValidationError
	for ni, n := range dsl.Nodes {
		for portKey, vs := range n.Inputs {
			if vs.Kind != workflow.ValueKindRef {
				continue
			}
			ref, ok := vs.Value.(workflow.RefValue)
			if !ok {
				continue
			}
			target, ok := nodeByID[ref.NodeID]
			if !ok {
				out = append(out, ValidationError{
					Code:    CodeDanglingRef,
					Message: fmt.Sprintf("node %q input %q references non-existent node %q", n.ID, portKey, ref.NodeID),
					Path:    fmt.Sprintf("nodes[%d].inputs.%s", ni, portKey),
				})
				continue
			}
			nt, ok := getType(target.TypeKey)
			if !ok {
				continue
			}
			portFound := false
			for _, p := range nt.OutputSchema {
				if p.ID == ref.PortID {
					portFound = true
					break
				}
			}
			if !portFound {
				out = append(out, ValidationError{
					Code:    CodeDanglingRef,
					Message: fmt.Sprintf("node %q input %q references unknown port %q on node %q", n.ID, portKey, ref.PortID, ref.NodeID),
					Path:    fmt.Sprintf("nodes[%d].inputs.%s", ni, portKey),
				})
			}
		}
	}
	return out
}

// 规则 5：Required=true 的输入端口必须绑了非空 ValueSource。
func checkRequiredInputs(
	dsl workflow.WorkflowDSL,
	getType func(string) (*nodetype.NodeType, bool),
) []ValidationError {
	var out []ValidationError
	for ni, n := range dsl.Nodes {
		nt, ok := getType(n.TypeKey)
		if !ok {
			continue
		}
		for _, p := range nt.InputSchema {
			if !p.Required {
				continue
			}
			vs, bound := n.Inputs[p.ID]
			if !bound || vs.Value == nil {
				out = append(out, ValidationError{
					Code:    CodeRequiredInputMissing,
					Message: fmt.Sprintf("node %q missing required input %q (%s)", n.ID, p.Name, p.ID),
					Path:    fmt.Sprintf("nodes[%d].inputs.%s", ni, p.ID),
				})
			}
		}
	}
	return out
}

// 规则 7：OnFinalFail=fallback 仅允许出现在声明了 PortDefault 端口的 NodeType 上。
func checkFallbackOnly(
	dsl workflow.WorkflowDSL,
	getType func(string) (*nodetype.NodeType, bool),
) []ValidationError {
	var out []ValidationError
	for ni, n := range dsl.Nodes {
		if n.ErrorPolicy == nil || n.ErrorPolicy.OnFinalFail != workflow.FailStrategyFallback {
			continue
		}
		nt, ok := getType(n.TypeKey)
		if !ok {
			continue
		}
		hasDefault := false
		for _, p := range nt.Ports {
			if p == workflow.PortDefault {
				hasDefault = true
				break
			}
		}
		if !hasDefault {
			out = append(out, ValidationError{
				Code:    CodeFallbackOnNonDefaultPortNode,
				Message: fmt.Sprintf("node %q uses fallback strategy but NodeType %q has no 'default' port", n.ID, n.TypeKey),
				Path:    fmt.Sprintf("nodes[%d].error_policy.on_final_fail", ni),
			})
		}
	}
	return out
}

// 规则 8：节点之间不存在控制流环。Kahn's algorithm（拓扑排序）实现。
func checkAcyclic(dsl workflow.WorkflowDSL, nodeByID map[string]*workflow.Node) []ValidationError {
	if len(dsl.Nodes) == 0 {
		return nil
	}
	indeg := make(map[string]int, len(dsl.Nodes))
	adj := make(map[string][]string, len(dsl.Nodes))
	for id := range nodeByID {
		indeg[id] = 0
	}
	for _, e := range dsl.Edges {
		if _, ok := nodeByID[e.From]; !ok {
			continue
		}
		if _, ok := nodeByID[e.To]; !ok {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		indeg[e.To]++
	}

	queue := make([]string, 0)
	for id, d := range indeg {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visited++
		for _, nxt := range adj[cur] {
			indeg[nxt]--
			if indeg[nxt] == 0 {
				queue = append(queue, nxt)
			}
		}
	}
	if visited != len(nodeByID) {
		return []ValidationError{{
			Code:    CodeCycle,
			Message: "DSL contains a cycle (v1 only allows loops via builtin.loop nodes)",
		}}
	}
	return nil
}
