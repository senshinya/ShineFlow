package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/infrastructure/util"
)

// ValueKind 区分 ValueSource 的求值策略。
type ValueKind string

const (
	ValueKindLiteral  ValueKind = "literal"
	ValueKindRef      ValueKind = "ref"
	ValueKindTemplate ValueKind = "template"
)

// ValueSource 是 Node.Inputs 中每个输入端口的取值描述。
//   - Kind == ValueKindLiteral  → Value 直接是字面量
//   - Kind == ValueKindRef      → Value 应能解为 RefValue
//   - Kind == ValueKindTemplate → Value 是含 {{var}} 的模板字符串
type ValueSource struct {
	Kind  ValueKind `json:"kind"`
	Value any       `json:"value"`
}

// RefValue 引用上游某个节点输出中的值（可选深路径）。
//   - NodeID 指向 DSL 内某个 Node.ID
//   - Path   在 object 类型 Output 上的深路径，如 "data.voice_url"；可为空
//   - Name   冗余的显示名，仅给前端读，不参与运行时解析
type RefValue struct {
	NodeID string `json:"node_id"`
	Path   string `json:"path,omitempty"`
	Name   string `json:"name,omitempty"`
}

// MarshalJSON 显式定义形态，锁定字段顺序、走 sonic。
func (v ValueSource) MarshalJSON() ([]byte, error) {
	s, err := util.MarshalToString(struct {
		Kind  ValueKind `json:"kind"`
		Value any       `json:"value"`
	}{v.Kind, v.Value})
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// UnmarshalJSON 按 Kind 分发，让 Value 解出正确的 Go 类型。
func (v *ValueSource) UnmarshalJSON(data []byte) error {
	var raw struct {
		Kind  ValueKind       `json:"kind"`
		Value json.RawMessage `json:"value"`
	}
	if err := util.UnmarshalFromString(string(data), &raw); err != nil {
		return err
	}
	v.Kind = raw.Kind
	switch raw.Kind {
	case ValueKindRef:
		var ref RefValue
		if err := util.UnmarshalFromString(string(raw.Value), &ref); err != nil {
			return fmt.Errorf("ref value: %w", err)
		}
		v.Value = ref
	case ValueKindTemplate:
		var s string
		if err := util.UnmarshalFromString(string(raw.Value), &s); err != nil {
			return fmt.Errorf("template value: %w", err)
		}
		v.Value = s
	case ValueKindLiteral:
		var any any
		if err := util.UnmarshalFromString(string(raw.Value), &any); err != nil {
			return fmt.Errorf("literal value: %w", err)
		}
		v.Value = any
	default:
		return fmt.Errorf("unknown value kind: %q", raw.Kind)
	}
	return nil
}
