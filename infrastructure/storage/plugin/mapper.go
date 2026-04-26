package plugin

import (
	domainplugin "github.com/shinya/shineflow/domain/plugin"
	domainworkflow "github.com/shinya/shineflow/domain/workflow"
)

// nonNilStringMap / nonNilPortSpecs 把 domain 的 nil map / slice 转成空 instance，
// 避免 NOT NULL JSONB 列写入失败（codec 的 Value 也会把 nil 兜底为 "{}"/"[]"，这里多一层保险）。
func nonNilStringMap(m map[string]string) stringMapColumn {
	if m == nil {
		return stringMapColumn{}
	}
	return stringMapColumn(m)
}

func nonNilPortSpecs(p []domainworkflow.PortSpec) portSpecsColumn {
	if p == nil {
		return portSpecsColumn{}
	}
	return portSpecsColumn(p)
}

func toHttpPlugin(m *httpPluginModel) *domainplugin.HttpPlugin {
	return &domainplugin.HttpPlugin{
		ID:              m.ID,
		Name:            m.Name,
		Description:     m.Description,
		Method:          m.Method,
		URL:             m.URL,
		Headers:         map[string]string(m.Headers),
		QueryParams:     map[string]string(m.QueryParams),
		BodyTemplate:    m.BodyTemplate,
		AuthKind:        m.AuthKind,
		CredentialID:    m.CredentialID,
		InputSchema:     []domainworkflow.PortSpec(m.InputSchema),
		OutputSchema:    []domainworkflow.PortSpec(m.OutputSchema),
		ResponseMapping: map[string]string(m.ResponseMapping),
		Enabled:         m.Enabled,
		CreatedBy:       m.CreatedBy,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

func toHttpPluginModel(p *domainplugin.HttpPlugin) *httpPluginModel {
	return &httpPluginModel{
		ID:              p.ID,
		Name:            p.Name,
		Description:     p.Description,
		Method:          p.Method,
		URL:             p.URL,
		Headers:         nonNilStringMap(p.Headers),
		QueryParams:     nonNilStringMap(p.QueryParams),
		BodyTemplate:    p.BodyTemplate,
		AuthKind:        p.AuthKind,
		CredentialID:    p.CredentialID,
		InputSchema:     nonNilPortSpecs(p.InputSchema),
		OutputSchema:    nonNilPortSpecs(p.OutputSchema),
		ResponseMapping: nonNilStringMap(p.ResponseMapping),
		Enabled:         p.Enabled,
		CreatedBy:       p.CreatedBy,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

func toMcpServer(m *mcpServerModel) *domainplugin.McpServer {
	return &domainplugin.McpServer{
		ID:            m.ID,
		Name:          m.Name,
		Transport:     domainplugin.McpTransport(m.Transport),
		Config:        m.Config,
		CredentialID:  m.CredentialID,
		Enabled:       m.Enabled,
		LastSyncedAt:  m.LastSyncedAt,
		LastSyncError: m.LastSyncError,
		CreatedBy:     m.CreatedBy,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}

func toMcpServerModel(s *domainplugin.McpServer) *mcpServerModel {
	return &mcpServerModel{
		ID:            s.ID,
		Name:          s.Name,
		Transport:     string(s.Transport),
		Config:        s.Config,
		CredentialID:  s.CredentialID,
		Enabled:       s.Enabled,
		LastSyncedAt:  s.LastSyncedAt,
		LastSyncError: s.LastSyncError,
		CreatedBy:     s.CreatedBy,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

func toMcpTool(m *mcpToolModel) *domainplugin.McpTool {
	return &domainplugin.McpTool{
		ID:             m.ID,
		ServerID:       m.ServerID,
		Name:           m.Name,
		Description:    m.Description,
		InputSchemaRaw: m.InputSchemaRaw,
		Enabled:        m.Enabled,
		SyncedAt:       m.SyncedAt,
	}
}

func toMcpToolModel(t *domainplugin.McpTool) *mcpToolModel {
	return &mcpToolModel{
		ID:             t.ID,
		ServerID:       t.ServerID,
		Name:           t.Name,
		Description:    t.Description,
		InputSchemaRaw: t.InputSchemaRaw,
		Enabled:        t.Enabled,
		SyncedAt:       t.SyncedAt,
	}
}
