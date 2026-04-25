package nodetype

// NodeTypeFilter 是 List 的过滤参数。空字段视为不约束。
type NodeTypeFilter struct {
	Category    string
	Builtin     *bool
	KeyPrefixes []string
}

// NodeTypeRegistry 是 NodeType 的统一查询入口。
//
// 实现要求（具体 impl 在 infrastructure 或 application 层）：
//   - Get 命中内置节点 → 返回静态 map 中的常量
//   - Get 命中 plugin.http.* → 调 HttpPluginRepository 后用 projectHttpPlugin 合成
//   - Get 命中 plugin.mcp.*.* → 调 McpServer + McpTool Repository 后用 projectMcpTool 合成
//   - 合成结果可缓存；HttpPlugin / McpServer / McpTool 变更时调 Invalidate / InvalidatePrefix 失效
type NodeTypeRegistry interface {
	Get(key string) (*NodeType, bool)
	List(filter NodeTypeFilter) []*NodeType

	Invalidate(key string)
	InvalidatePrefix(prefix string)
}
