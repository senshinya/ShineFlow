// Package validator 实现 WorkflowDSL 的严格校验（PublishVersion 时必过）。
//
// 本包独立于 domain/workflow，避免引入 workflow → nodetype → workflow 的循环依赖。
package validator

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

// ValidationError 描述一条校验违例。Code 是可机读分类，Message 是给用户的可读描述，
// Path 指向出错位置（如 "nodes[2].inputs.in_prompt"），可为空。
type ValidationError struct {
	NodeID  string
	Code    string
	Message string
	Path    string
}

// 校验违例 Code 常量。
const (
	CodeMissingStart                   = "missing_start"
	CodeMissingEnd                     = "missing_end"
	CodeDuplicateNodeID                = "duplicate_node_id"
	CodeDuplicateEdgeID                = "duplicate_edge_id"
	CodeDuplicatePortID                = "duplicate_port_id"
	CodeDanglingEdge                   = "dangling_edge"
	CodeDanglingRef                    = "dangling_ref"
	CodeUnknownFromPort                = "unknown_from_port"
	CodeRequiredInputMissing           = "required_input_missing"
	CodeUnknownNodeType                = "unknown_node_type"
	CodeFallbackOnNonDefaultPortNode   = "fallback_on_non_default_port_node"
	CodeCycle                          = "cycle"
	CodeMultipleStarts                 = "multiple_starts"
	CodeNoPathToEnd                    = "no_path_to_end"
	CodeIsolatedNode                   = "isolated_node"
	CodeMultiInputRequiresJoin         = "multi_input_requires_join"
	CodeJoinInsufficientInputs         = "join_insufficient_inputs"
	CodeJoinModeInvalid                = "join_mode_invalid"
	CodeJoinConfigInvalid              = "join_config_invalid"
	CodeSwitchConfigInvalid            = "switch_config_invalid"
	CodeSwitchCaseNameDuplicate        = "switch_case_name_duplicate"
	CodeSwitchCaseNameReserved         = "switch_case_name_reserved"
	CodeFallbackPortInvalid            = "fallback_port_invalid"
	CodeFireErrorPortRequiresErrorPort = "fire_error_port_requires_error_port"
)

// ValidationResult 是 ValidateForPublish 的返回值，一次性收集所有违例。
type ValidationResult struct {
	Errors []ValidationError
}

// OK 是否通过校验。
func (r ValidationResult) OK() bool { return len(r.Errors) == 0 }

// Validate 返回全部校验错误，便于引擎和测试直接使用。
func Validate(dsl workflow.WorkflowDSL, reg nodetype.NodeTypeRegistry) []ValidationError {
	return ValidateForPublish(dsl, reg).Errors
}

// ValidateForPublish 对一个 WorkflowDSL 做发布前严格校验，收集所有违例后一并返回。
func ValidateForPublish(dsl workflow.WorkflowDSL, reg nodetype.NodeTypeRegistry) ValidationResult {
	var errs []ValidationError

	errs = append(errs, checkStartEnd(dsl)...)
	errs = append(errs, checkSingleStart(dsl)...)
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
	errs = append(errs, checkEdgeFromPorts(dsl, nodeByID, reg)...)
	errs = append(errs, checkRefValues(dsl, nodeByID)...)
	errs = append(errs, checkRequiredInputs(dsl, getType)...)
	errs = append(errs, checkNoPathToEnd(dsl)...)
	errs = append(errs, checkIsolatedNode(dsl)...)
	errs = append(errs, checkMultiInputRequiresJoin(dsl)...)
	errs = append(errs, checkJoin(dsl)...)
	errs = append(errs, checkSwitchCaseNames(dsl)...)
	errs = append(errs, checkFallbackPort(dsl, reg)...)
	errs = append(errs, checkFireErrorPortRequiresErrorPort(dsl, reg)...)
	errs = append(errs, checkAcyclic(dsl, nodeByID)...)

	return ValidationResult{Errors: errs}
}

// 规则：至少 1 个 builtin.start，至少 1 个 builtin.end。
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

// 规则：最多 1 个 builtin.start。
func checkSingleStart(dsl workflow.WorkflowDSL) []ValidationError {
	count := 0
	firstID := ""
	for _, n := range dsl.Nodes {
		if n.TypeKey != nodetype.BuiltinStart {
			continue
		}
		count++
		if firstID == "" {
			firstID = n.ID
		}
	}
	if count > 1 {
		return []ValidationError{{
			NodeID:  firstID,
			Code:    CodeMultipleStarts,
			Message: "DSL must contain at most one Start node",
		}}
	}
	return nil
}

// 规则：Node.ID / Edge.ID / 单节点内输入 key 唯一。
func checkUniqueIDs(dsl workflow.WorkflowDSL) []ValidationError {
	var out []ValidationError
	seenN := map[string]int{}
	for i, n := range dsl.Nodes {
		if first, ok := seenN[n.ID]; ok {
			out = append(out, ValidationError{
				NodeID:  n.ID,
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
	return out
}

// 规则：Node.TypeKey 必须能在 Registry 解析到。
func checkNodeTypesExist(dsl workflow.WorkflowDSL, getType func(string) (*nodetype.NodeType, bool)) []ValidationError {
	var out []ValidationError
	for i, n := range dsl.Nodes {
		if _, ok := getType(n.TypeKey); !ok {
			out = append(out, ValidationError{
				NodeID:  n.ID,
				Code:    CodeUnknownNodeType,
				Message: fmt.Sprintf("unknown NodeType %q on node %q", n.TypeKey, n.ID),
				Path:    fmt.Sprintf("nodes[%d].type_key", i),
			})
		}
	}
	return out
}

// 规则：Edge.From / To 必须指向 DSL 内真实节点。
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

// 规则：Edge.FromPort 必须是源节点的真实输出端口。
func checkEdgeFromPorts(dsl workflow.WorkflowDSL, nodeByID map[string]*workflow.Node, ntReg nodetype.NodeTypeRegistry) []ValidationError {
	var out []ValidationError
	for i, e := range dsl.Edges {
		src, ok := nodeByID[e.From]
		if !ok {
			continue
		}
		if src.TypeKey != nodetype.BuiltinSwitch {
			if _, ok := ntReg.Get(src.TypeKey); !ok {
				continue
			}
		}
		ports := outputPortsOf(src, ntReg)
		if !containsString(ports, e.FromPort) {
			out = append(out, ValidationError{
				NodeID:  src.ID,
				Code:    CodeUnknownFromPort,
				Message: fmt.Sprintf("edge %q uses port %q not declared by NodeType %q", e.ID, e.FromPort, src.TypeKey),
				Path:    fmt.Sprintf("edges[%d].from_port", i),
			})
		}
	}
	return out
}

// 规则：RefValue.NodeID 必须指向 DSL 内真实节点。
func checkRefValues(dsl workflow.WorkflowDSL, nodeByID map[string]*workflow.Node) []ValidationError {
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
			if _, ok := nodeByID[ref.NodeID]; !ok {
				out = append(out, ValidationError{
					NodeID:  n.ID,
					Code:    CodeDanglingRef,
					Message: fmt.Sprintf("node %q input %q references non-existent node %q", n.ID, portKey, ref.NodeID),
					Path:    fmt.Sprintf("nodes[%d].inputs.%s", ni, portKey),
				})
			}
		}
	}
	return out
}

// 规则：Required=true 的输入端口必须绑了非空 ValueSource。
func checkRequiredInputs(dsl workflow.WorkflowDSL, getType func(string) (*nodetype.NodeType, bool)) []ValidationError {
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
					NodeID:  n.ID,
					Code:    CodeRequiredInputMissing,
					Message: fmt.Sprintf("node %q missing required input %q (%s)", n.ID, p.Name, p.ID),
					Path:    fmt.Sprintf("nodes[%d].inputs.%s", ni, p.ID),
				})
			}
		}
	}
	return out
}

// 规则：OnFinalFail=fallback 仅允许出现在声明 default 端口的 NodeType 上。
func checkFallbackOnly(dsl workflow.WorkflowDSL, getType func(string) (*nodetype.NodeType, bool)) []ValidationError {
	var out []ValidationError
	for ni, n := range dsl.Nodes {
		if n.ErrorPolicy == nil || n.ErrorPolicy.OnFinalFail != workflow.FailStrategyFallback {
			continue
		}
		nt, ok := getType(n.TypeKey)
		if !ok {
			continue
		}
		if !containsString(nt.Ports, workflow.PortDefault) {
			out = append(out, ValidationError{
				NodeID:  n.ID,
				Code:    CodeFallbackOnNonDefaultPortNode,
				Message: fmt.Sprintf("node %q uses fallback strategy but NodeType %q has no 'default' port", n.ID, n.TypeKey),
				Path:    fmt.Sprintf("nodes[%d].error_policy.on_final_fail", ni),
			})
		}
	}
	return out
}

// 规则：至少存在一条 Start 到 End 的有向路径；旁路 fire-and-forget 分支合法。
func checkNoPathToEnd(dsl workflow.WorkflowDSL) []ValidationError {
	var starts, ends []string
	for _, n := range dsl.Nodes {
		switch n.TypeKey {
		case nodetype.BuiltinStart:
			starts = append(starts, n.ID)
		case nodetype.BuiltinEnd:
			ends = append(ends, n.ID)
		}
	}
	if len(starts) == 0 || len(ends) == 0 {
		return nil
	}

	adj := map[string][]string{}
	for _, e := range dsl.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	seen := map[string]bool{}
	queue := make([]string, 0, len(starts))
	for _, s := range starts {
		seen[s] = true
		queue = append(queue, s)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nxt := range adj[cur] {
			if !seen[nxt] {
				seen[nxt] = true
				queue = append(queue, nxt)
			}
		}
	}
	for _, e := range ends {
		if seen[e] {
			return nil
		}
	}
	return []ValidationError{{Code: CodeNoPathToEnd, Message: "no directed path from any Start to any End node"}}
}

// 规则：非 Start 节点必须至少有一条入边。
func checkIsolatedNode(dsl workflow.WorkflowDSL) []ValidationError {
	inDeg := map[string]int{}
	for _, e := range dsl.Edges {
		inDeg[e.To]++
	}
	var errs []ValidationError
	for _, n := range dsl.Nodes {
		if inDeg[n.ID] == 0 && n.TypeKey != nodetype.BuiltinStart {
			errs = append(errs, ValidationError{
				NodeID:  n.ID,
				Code:    CodeIsolatedNode,
				Message: fmt.Sprintf("node %s has no inbound edges and is not a Start", n.ID),
			})
		}
	}
	return errs
}

// 规则：多输入节点必须显式使用 builtin.join。
func checkMultiInputRequiresJoin(dsl workflow.WorkflowDSL) []ValidationError {
	inDeg := map[string]int{}
	for _, e := range dsl.Edges {
		inDeg[e.To]++
	}
	var errs []ValidationError
	for _, n := range dsl.Nodes {
		if inDeg[n.ID] > 1 && n.TypeKey != nodetype.BuiltinJoin {
			errs = append(errs, ValidationError{
				NodeID:  n.ID,
				Code:    CodeMultiInputRequiresJoin,
				Message: fmt.Sprintf("node %s has %d inbound edges; multi-input nodes must be builtin.join", n.ID, inDeg[n.ID]),
			})
		}
	}
	return errs
}

type joinConfig struct {
	Mode string `json:"mode"`
}

// 规则：join 必须有足够输入，且 Config.mode 合法。
func checkJoin(dsl workflow.WorkflowDSL) []ValidationError {
	inDeg := map[string]int{}
	for _, e := range dsl.Edges {
		inDeg[e.To]++
	}
	var errs []ValidationError
	for _, n := range dsl.Nodes {
		if n.TypeKey != nodetype.BuiltinJoin {
			continue
		}
		if inDeg[n.ID] < 2 {
			errs = append(errs, ValidationError{
				NodeID:  n.ID,
				Code:    CodeJoinInsufficientInputs,
				Message: fmt.Sprintf("builtin.join requires >=2 inputs, got %d", inDeg[n.ID]),
			})
		}
		var cfg joinConfig
		if err := json.Unmarshal(n.Config, &cfg); err != nil {
			errs = append(errs, ValidationError{
				NodeID:  n.ID,
				Code:    CodeJoinConfigInvalid,
				Message: fmt.Sprintf("builtin.join Config invalid: %v", err),
			})
			continue
		}
		if cfg.Mode != nodetype.JoinModeAny && cfg.Mode != nodetype.JoinModeAll {
			errs = append(errs, ValidationError{
				NodeID: n.ID,
				Code:   CodeJoinModeInvalid,
				Message: fmt.Sprintf("builtin.join mode must be %q or %q, got %q",
					nodetype.JoinModeAny, nodetype.JoinModeAll, cfg.Mode),
			})
		}
	}
	return errs
}

type switchCase struct {
	Name string `json:"name"`
}

type switchConfig struct {
	Cases []switchCase `json:"cases"`
}

var portNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// 规则：switch case 名不能重复、不能占用保留端口、必须符合端口名格式。
func checkSwitchCaseNames(dsl workflow.WorkflowDSL) []ValidationError {
	var errs []ValidationError
	for _, n := range dsl.Nodes {
		if n.TypeKey != nodetype.BuiltinSwitch {
			continue
		}
		var cfg switchConfig
		if err := json.Unmarshal(n.Config, &cfg); err != nil {
			errs = append(errs, ValidationError{
				NodeID:  n.ID,
				Code:    CodeSwitchConfigInvalid,
				Message: fmt.Sprintf("builtin.switch Config invalid: %v", err),
			})
			continue
		}
		seen := map[string]bool{}
		for _, c := range cfg.Cases {
			if c.Name == workflow.PortDefault || c.Name == workflow.PortError || !portNamePattern.MatchString(c.Name) {
				errs = append(errs, ValidationError{
					NodeID:  n.ID,
					Code:    CodeSwitchCaseNameReserved,
					Message: fmt.Sprintf("switch case name %q is invalid", c.Name),
				})
				continue
			}
			if seen[c.Name] {
				errs = append(errs, ValidationError{
					NodeID:  n.ID,
					Code:    CodeSwitchCaseNameDuplicate,
					Message: fmt.Sprintf("switch case name %q duplicated", c.Name),
				})
			}
			seen[c.Name] = true
		}
	}
	return errs
}

// 规则：fallback 必须声明一个真实输出端口。
func checkFallbackPort(dsl workflow.WorkflowDSL, ntReg nodetype.NodeTypeRegistry) []ValidationError {
	var errs []ValidationError
	for i := range dsl.Nodes {
		n := &dsl.Nodes[i]
		if n.ErrorPolicy == nil || n.ErrorPolicy.OnFinalFail != workflow.FailStrategyFallback {
			continue
		}
		port := n.ErrorPolicy.FallbackOutput.Port
		if port == "" {
			errs = append(errs, ValidationError{
				NodeID:  n.ID,
				Code:    CodeFallbackPortInvalid,
				Message: "FallbackOutput.Port required when OnFinalFail=Fallback",
			})
			continue
		}
		ports := outputPortsOf(n, ntReg)
		if !containsString(ports, port) {
			errs = append(errs, ValidationError{
				NodeID:  n.ID,
				Code:    CodeFallbackPortInvalid,
				Message: fmt.Sprintf("FallbackOutput.Port %q not in node ports %v", port, ports),
			})
		}
	}
	return errs
}

// 规则：FireErrorPort 只能用于具有 error 输出端口的节点。
func checkFireErrorPortRequiresErrorPort(dsl workflow.WorkflowDSL, ntReg nodetype.NodeTypeRegistry) []ValidationError {
	var errs []ValidationError
	for i := range dsl.Nodes {
		n := &dsl.Nodes[i]
		if n.ErrorPolicy == nil || n.ErrorPolicy.OnFinalFail != workflow.FailStrategyFireErrorPort {
			continue
		}
		ports := outputPortsOf(n, ntReg)
		if !containsString(ports, workflow.PortError) {
			errs = append(errs, ValidationError{
				NodeID:  n.ID,
				Code:    CodeFireErrorPortRequiresErrorPort,
				Message: fmt.Sprintf("OnFinalFail=FireErrorPort but node has no 'error' output port (ports=%v)", ports),
			})
		}
	}
	return errs
}

// 规则：节点之间不存在控制流环。Kahn 拓扑排序实现。
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
		return []ValidationError{{Code: CodeCycle, Message: "DSL contains a cycle (v1 only allows loops via builtin.loop nodes)"}}
	}
	return nil
}

// outputPortsOf 返回节点的真实输出端口集；switch 会按 Config.cases 动态展开。
func outputPortsOf(node *workflow.Node, ntReg nodetype.NodeTypeRegistry) []string {
	if node.TypeKey == nodetype.BuiltinSwitch {
		var cfg switchConfig
		_ = json.Unmarshal(node.Config, &cfg)
		ports := make([]string, 0, len(cfg.Cases)+2)
		for _, c := range cfg.Cases {
			ports = append(ports, c.Name)
		}
		return append(ports, workflow.PortDefault, workflow.PortError)
	}
	nt, ok := ntReg.Get(node.TypeKey)
	if !ok {
		return nil
	}
	return nt.Ports
}

func containsString(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}
