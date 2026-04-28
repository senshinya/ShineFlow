package nodetype

// 内置 NodeType 的 Key 常量。由代码 init 时静态注册到 Registry。
const (
	BuiltinStart        = "builtin.start"
	BuiltinEnd          = "builtin.end"
	BuiltinLLM          = "builtin.llm"
	BuiltinIf           = "builtin.if"
	BuiltinSwitch       = "builtin.switch"
	BuiltinJoin         = "builtin.join"
	BuiltinLoop         = "builtin.loop"
	BuiltinCode         = "builtin.code"
	BuiltinSetVariable  = "builtin.set_variable"
	BuiltinHTTPRequest  = "builtin.http_request"
)

// Join 节点支持的汇合模式。
const (
	JoinModeAny = "any"
	JoinModeAll = "all"
)

// 插件 NodeType Key 的前缀；用于 Registry 投影 / 失效检索。
const (
	PluginHTTPPrefix = "plugin.http." // plugin.http.<HttpPlugin.ID>
	PluginMCPPrefix  = "plugin.mcp."  // plugin.mcp.<McpServer.ID>.<McpTool.Name>
)

// If 节点的两条非默认控制端口名。
// 其余保留端口（default / error）用 workflow.PortDefault / workflow.PortError，避免重复常量。
const (
	PortIfTrue  = "true"
	PortIfFalse = "false"
)
