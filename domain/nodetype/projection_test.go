package nodetype

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/shinya/shineflow/domain/plugin"
	"github.com/shinya/shineflow/domain/workflow"
)

func TestProjectHttpPlugin(t *testing.T) {
	p := &plugin.HttpPlugin{
		ID:          "hp_001",
		Name:        "Translate API",
		Description: "translate text",
		InputSchema: []workflow.PortSpec{
			{ID: "in_1", Name: "text", Type: workflow.SchemaType{Type: workflow.SchemaTypeString}, Required: true},
		},
		OutputSchema: []workflow.PortSpec{
			{ID: "out_1", Name: "translated", Type: workflow.SchemaType{Type: workflow.SchemaTypeString}},
		},
	}

	got := ProjectHttpPlugin(p)

	if got.Key != "plugin.http.hp_001" {
		t.Fatalf("Key = %q, want %q", got.Key, "plugin.http.hp_001")
	}
	if got.Version != NodeTypeVersion1 {
		t.Errorf("Version = %q, want %q", got.Version, NodeTypeVersion1)
	}
	if got.Name != p.Name || got.Description != p.Description {
		t.Errorf("Name/Description not propagated; got=%+v", got)
	}
	if got.Category != CategoryTool {
		t.Errorf("Category = %q, want %q", got.Category, CategoryTool)
	}
	if got.Builtin {
		t.Error("Builtin should be false for plugin")
	}
	if string(got.ConfigSchema) != "{}" {
		t.Errorf("ConfigSchema = %s, want '{}'", got.ConfigSchema)
	}
	if !reflect.DeepEqual(got.InputSchema, p.InputSchema) {
		t.Errorf("InputSchema mismatch")
	}
	if !reflect.DeepEqual(got.OutputSchema, p.OutputSchema) {
		t.Errorf("OutputSchema mismatch")
	}
	wantPorts := []string{workflow.PortDefault, workflow.PortError}
	if !reflect.DeepEqual(got.Ports, wantPorts) {
		t.Errorf("Ports = %v, want %v", got.Ports, wantPorts)
	}
}

func TestProjectMcpTool(t *testing.T) {
	server := &plugin.McpServer{ID: "svr_1", Name: "FS"}
	tool := &plugin.McpTool{
		ServerID:    "svr_1",
		Name:        "read_file",
		Description: "read a file",
		InputSchemaRaw: json.RawMessage(`{
			"type":"object",
			"properties":{"path":{"type":"string"}},
			"required":["path"]
		}`),
	}

	got := ProjectMcpTool(tool, server)

	if got.Key != "plugin.mcp.svr_1.read_file" {
		t.Fatalf("Key = %q", got.Key)
	}
	if got.Name != "FS / read_file" {
		t.Errorf("Name = %q, want %q", got.Name, "FS / read_file")
	}
	if got.Description != tool.Description {
		t.Errorf("Description not propagated")
	}
	if got.Category != CategoryTool || got.Builtin {
		t.Errorf("Category/Builtin wrong: %+v", got)
	}
	if len(got.OutputSchema) != 1 || got.OutputSchema[0].Name != "result" {
		t.Errorf("OutputSchema should be a single 'result' port, got %+v", got.OutputSchema)
	}
	if got.OutputSchema[0].ID == "" {
		t.Error("OutputSchema port ID should be a stable hash, not empty")
	}
	wantPorts := []string{workflow.PortDefault, workflow.PortError}
	if !reflect.DeepEqual(got.Ports, wantPorts) {
		t.Errorf("Ports = %v, want %v", got.Ports, wantPorts)
	}
	if len(got.InputSchema) != 1 || got.InputSchema[0].Name != "path" || !got.InputSchema[0].Required {
		t.Errorf("InputSchema降维结果不符: %+v", got.InputSchema)
	}
}

func TestMcpSchemaToPortSpecs_FlatObject(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"object",
		"properties":{
			"a":{"type":"string","description":"hello"},
			"b":{"type":"integer"}
		},
		"required":["a"]
	}`)
	ports := mcpSchemaToPortSpecs(raw)

	if len(ports) != 2 {
		t.Fatalf("len = %d, want 2", len(ports))
	}
	byName := map[string]workflow.PortSpec{}
	for _, p := range ports {
		byName[p.Name] = p
	}
	if !byName["a"].Required || byName["a"].Type.Type != workflow.SchemaTypeString || byName["a"].Desc != "hello" {
		t.Errorf("port a wrong: %+v", byName["a"])
	}
	if byName["b"].Required || byName["b"].Type.Type != workflow.SchemaTypeInteger {
		t.Errorf("port b wrong: %+v", byName["b"])
	}
}

func TestMcpSchemaToPortSpecs_NotObject(t *testing.T) {
	// 顶层不是 object 时降维结果应是空切片（MCP tool 的 inputSchema 总应该是 object）
	raw := json.RawMessage(`{"type":"string"}`)
	ports := mcpSchemaToPortSpecs(raw)
	if len(ports) != 0 {
		t.Errorf("len = %d, want 0", len(ports))
	}
}
