package executor

import "github.com/shinya/shineflow/domain/nodetype"

// ExecutorFactory 用 NodeType 构造 NodeExecutor。
// 同一 keyPattern 下所有 NodeType 共享一个 factory；factory 内部可按 NodeType 字段做差异化。
type ExecutorFactory func(nt *nodetype.NodeType) NodeExecutor

// ExecutorRegistry 是 NodeType.Key → NodeExecutor 的映射注册表。
//
// keyPattern 形态：
//   - 精确：              "builtin.llm"
//   - 前缀通配（按段）：  "plugin.http.*" / "plugin.mcp.*.*"
//
// 匹配优先级：
//  1. 精确 key 命中
//  2. 前缀通配中"字面前缀最长"的 pattern（即 pattern 中 '*' 之前的字符串最长）
//  3. 段数必须匹配："plugin.mcp.*.*" 不会匹配 "plugin.mcp.svr_1"（段数差）
type ExecutorRegistry interface {
	Register(keyPattern string, factory ExecutorFactory)
	Build(nt *nodetype.NodeType) (NodeExecutor, error)
}
