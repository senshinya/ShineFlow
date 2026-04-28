package run

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Symbols 是单次运行的变量命名空间，供模板和引用解析使用。
//
// 三个根命名空间：
//   - trigger.<key>：触发 payload，顶层必须是 JSON object
//   - vars.<key>：set_variable 节点累计写入
//   - nodes.<nodeID>.<key>：成功节点或 fallback 节点的输出
//
// 内部用 json.RawMessage 保存：写入时序列化，读取时按需反序列化，保证调用方拿到独立对象。
type Symbols struct {
	trigger json.RawMessage
	vars    map[string]json.RawMessage
	nodes   map[string]json.RawMessage
}

// NewSymbols 校验触发 payload 为 JSON object，并初始化空 vars/nodes。
func NewSymbols(payload json.RawMessage) (*Symbols, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(payload, &probe); err != nil {
		return nil, fmt.Errorf("trigger payload must be a JSON object: %w", err)
	}
	if probe == nil {
		return nil, fmt.Errorf("trigger payload must be a JSON object")
	}
	return &Symbols{
		trigger: payload,
		vars:    map[string]json.RawMessage{},
		nodes:   map[string]json.RawMessage{},
	}, nil
}

// SetNodeOutput 将节点输出序列化写入 nodes[nodeID]；nil 输出按空对象处理。
func (s *Symbols) SetNodeOutput(nodeID string, output map[string]any) error {
	if output == nil {
		s.nodes[nodeID] = json.RawMessage(`{}`)
		return nil
	}
	b, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshal node output %s: %w", nodeID, err)
	}
	s.nodes[nodeID] = b
	return nil
}

// SetVar 将变量值序列化写入 vars[key]。
func (s *Symbols) SetVar(key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal var %s: %w", key, err)
	}
	s.vars[key] = b
	return nil
}

// SnapshotVars 返回 vars map 的新表头，调用方可修改返回 map。
func (s *Symbols) SnapshotVars() map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(s.vars))
	for k, v := range s.vars {
		out[k] = v
	}
	return out
}

// Snapshot 克隆 map 表头并共享 RawMessage 值，用于获得调度时刻视图。
func (s *Symbols) Snapshot() *Symbols {
	vars := make(map[string]json.RawMessage, len(s.vars))
	for k, v := range s.vars {
		vars[k] = v
	}
	nodes := make(map[string]json.RawMessage, len(s.nodes))
	for k, v := range s.nodes {
		nodes[k] = v
	}
	return &Symbols{trigger: s.trigger, vars: vars, nodes: nodes}
}

// Lookup 解析点分路径；每次读取都会反序列化子树，调用方可安全修改返回对象。
func (s *Symbols) Lookup(path string) (any, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	parts := strings.Split(path, ".")
	if parts[0] == "" {
		return nil, fmt.Errorf("empty path")
	}

	var raw json.RawMessage
	var rest []string
	switch parts[0] {
	case "trigger":
		raw, rest = s.trigger, parts[1:]
	case "vars":
		if len(parts) < 2 {
			return nil, fmt.Errorf("vars.<key> required")
		}
		v, ok := s.vars[parts[1]]
		if !ok {
			return nil, fmt.Errorf("var not set: %s", parts[1])
		}
		raw, rest = v, parts[2:]
	case "nodes":
		if len(parts) < 2 {
			return nil, fmt.Errorf("nodes.<id> required")
		}
		n, ok := s.nodes[parts[1]]
		if !ok {
			return nil, fmt.Errorf("node not yet produced output: %s", parts[1])
		}
		raw, rest = n, parts[2:]
	default:
		return nil, fmt.Errorf("unknown root: %q", parts[0])
	}

	var cur any
	if err := json.Unmarshal(raw, &cur); err != nil {
		return nil, fmt.Errorf("symbols decode at %q: %w", parts[0], err)
	}
	return walkPath(cur, rest)
}

func walkPath(cur any, parts []string) (any, error) {
	for _, p := range parts {
		switch x := cur.(type) {
		case map[string]any:
			v, ok := x[p]
			if !ok {
				return nil, fmt.Errorf("key not found: %s", p)
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(p)
			if err != nil || idx < 0 || idx >= len(x) {
				return nil, fmt.Errorf("invalid array index: %s", p)
			}
			cur = x[idx]
		default:
			return nil, fmt.Errorf("cannot navigate %T at %q", cur, p)
		}
	}
	return cur, nil
}
