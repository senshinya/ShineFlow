package nodetype

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shinya/shineflow/domain/plugin"
	"github.com/shinya/shineflow/domain/workflow"
)

// ProjectHttpPlugin 把 HttpPlugin 投影成 NodeType。
//
// 约束：
//   - HttpPlugin 自身已配置好请求骨架，故 ConfigSchema 固定为空对象 "{}"
//   - InputSchema / OutputSchema 直接透传
//   - Ports 固定 [default, error]
func ProjectHttpPlugin(p *plugin.HttpPlugin) *NodeType {
	return &NodeType{
		Key:          PluginHTTPPrefix + p.ID,
		Version:      NodeTypeVersion1,
		Name:         p.Name,
		Description:  p.Description,
		Category:     CategoryTool,
		Builtin:      false,
		ConfigSchema: json.RawMessage(`{}`),
		InputSchema:  p.InputSchema,
		OutputSchema: p.OutputSchema,
		Ports:        []string{workflow.PortDefault, workflow.PortError},
	}
}

// ProjectMcpTool 把 (McpTool, McpServer) 投影成 NodeType。
//
// 约束：
//   - InputSchema 由 mcpSchemaToPortSpecs 把 MCP 原生 JSON Schema 顶层 properties 降维而来
//   - OutputSchema 固定为单端口 "result"，类型 object；ID 由 (server.ID, tool.Name) 派生稳定 hash
func ProjectMcpTool(t *plugin.McpTool, s *plugin.McpServer) *NodeType {
	return &NodeType{
		Key:          fmt.Sprintf("%s%s.%s", PluginMCPPrefix, s.ID, t.Name),
		Version:      NodeTypeVersion1,
		Name:         fmt.Sprintf("%s / %s", s.Name, t.Name),
		Description:  t.Description,
		Category:     CategoryTool,
		Builtin:      false,
		ConfigSchema: json.RawMessage(`{}`),
		InputSchema:  mcpSchemaToPortSpecs(t.InputSchemaRaw),
		OutputSchema: []workflow.PortSpec{{
			ID:   "mcp_result_" + stableHash(s.ID+":"+t.Name),
			Name: "result",
			Type: workflow.SchemaType{Type: workflow.SchemaTypeObject},
		}},
		Ports: []string{workflow.PortDefault, workflow.PortError},
	}
}

// mcpSchemaToPortSpecs 把 MCP tool 的原生 JSON Schema（顶层应为 object）的 properties
// 降维成 []PortSpec。每个顶层 property 对应一个 PortSpec，类型只取 type 字段（嵌套留给 SchemaType）。
//
// PortSpec.ID 形如 "mcp_in_<sha1(name)前 8 字节>"，与 name 一一对应、可稳定回查。
// 若 raw 不是 object 或解析失败，返回 nil。
func mcpSchemaToPortSpecs(raw json.RawMessage) []workflow.PortSpec {
	if len(raw) == 0 {
		return nil
	}
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil
	}
	if schema.Type != workflow.SchemaTypeObject {
		return nil
	}

	requiredSet := map[string]bool{}
	for _, r := range schema.Required {
		requiredSet[r] = true
	}

	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]workflow.PortSpec, 0, len(names))
	for _, name := range names {
		var prop struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		}
		_ = json.Unmarshal(schema.Properties[name], &prop)
		out = append(out, workflow.PortSpec{
			ID:       "mcp_in_" + stableHash(name),
			Name:     name,
			Type:     workflow.SchemaType{Type: prop.Type},
			Required: requiredSet[name],
			Desc:     prop.Description,
		})
	}
	return out
}

func stableHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}
