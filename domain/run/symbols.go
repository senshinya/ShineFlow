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
