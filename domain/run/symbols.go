package run

import (
	"encoding/json"
	"fmt"
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
