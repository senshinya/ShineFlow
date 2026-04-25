package run

import (
	"encoding/json"
	"fmt"
)

// BuildContext 将 WorkflowRun 的触发参数 / Vars / NodeRun Output 投影成扁平变量表。
//
// 输出 key 的前缀规则（与 spec §6.5 / §8.4 对齐）：
//
//   trigger.<top>             ← WorkflowRun.TriggerPayload 顶层字段（必为 JSON object）
//   vars.<top>                ← WorkflowRun.Vars 顶层字段；nil/空时跳过
//   nodes.<nodeID>.<top>      ← 每个 NodeID 取 Attempt 最大且 (Status==Success 或 FallbackApplied)
//                                的 NodeRun.Output 顶层字段
//
// 为何"取最大 Attempt 而不是最后一次 success"：FallbackApplied=true 时 Status=Failed，但 Output
// 是兜底值，下游期望读到这个兜底；规则统一为"按 Attempt 排序、取头部、判断是否可计入"。
//
// TriggerPayload / Vars / Output 都必须是 JSON object，否则返回错误（不支持顶层数组 / 标量）。
// 这是引擎契约，避免 {{trigger}} 这种"整体引用"的歧义。
func BuildContext(run *WorkflowRun, nodeRuns []*NodeRun) (map[string]any, error) {
	out := make(map[string]any)

	if err := flattenInto(out, "trigger", run.TriggerPayload); err != nil {
		return nil, fmt.Errorf("trigger payload: %w", err)
	}
	if len(run.Vars) > 0 {
		if err := flattenInto(out, "vars", run.Vars); err != nil {
			return nil, fmt.Errorf("vars: %w", err)
		}
	}

	// 选择每个 NodeID 的"最新可用" attempt
	type pick struct {
		attempt int
		nr      *NodeRun
	}
	latest := map[string]pick{}
	for _, nr := range nodeRuns {
		cur, ok := latest[nr.NodeID]
		if !ok || nr.Attempt > cur.attempt {
			latest[nr.NodeID] = pick{attempt: nr.Attempt, nr: nr}
		}
	}
	for nodeID, p := range latest {
		nr := p.nr
		usable := nr.Status == NodeRunStatusSuccess || nr.FallbackApplied
		if !usable || len(nr.Output) == 0 {
			continue
		}
		if err := flattenInto(out, "nodes."+nodeID, nr.Output); err != nil {
			return nil, fmt.Errorf("node %s output: %w", nodeID, err)
		}
	}
	return out, nil
}

// flattenInto 把 raw（必须是 JSON object）的顶层每个 key 写入 dest 的 "<prefix>.<key>"。
func flattenInto(dest map[string]any, prefix string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("expected JSON object: %w", err)
	}
	for k, v := range obj {
		dest[prefix+"."+k] = v
	}
	return nil
}
