package workflow

import domainworkflow "github.com/shinya/shineflow/domain/workflow"

func toDefinition(m *definitionModel) *domainworkflow.WorkflowDefinition {
	return &domainworkflow.WorkflowDefinition{
		ID:                 m.ID,
		Name:               m.Name,
		Description:        m.Description,
		DraftVersionID:     m.DraftVersionID,
		PublishedVersionID: m.PublishedVersionID,
		CreatedBy:          m.CreatedBy,
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
	}
}

func toDefinitionModel(d *domainworkflow.WorkflowDefinition) *definitionModel {
	return &definitionModel{
		ID:                 d.ID,
		Name:               d.Name,
		Description:        d.Description,
		DraftVersionID:     d.DraftVersionID,
		PublishedVersionID: d.PublishedVersionID,
		CreatedBy:          d.CreatedBy,
		CreatedAt:          d.CreatedAt,
		UpdatedAt:          d.UpdatedAt,
	}
}

func toVersion(m *versionModel) *domainworkflow.WorkflowVersion {
	return &domainworkflow.WorkflowVersion{
		ID:           m.ID,
		DefinitionID: m.DefinitionID,
		Version:      m.Version,
		State:        domainworkflow.VersionState(m.State),
		DSL:          domainworkflow.WorkflowDSL(m.DSL),
		Revision:     m.Revision,
		PublishedAt:  m.PublishedAt,
		PublishedBy:  m.PublishedBy,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

func toVersionModel(v *domainworkflow.WorkflowVersion) *versionModel {
	return &versionModel{
		ID:           v.ID,
		DefinitionID: v.DefinitionID,
		Version:      v.Version,
		State:        string(v.State),
		DSL:          dslColumn(v.DSL),
		Revision:     v.Revision,
		PublishedAt:  v.PublishedAt,
		PublishedBy:  v.PublishedBy,
		CreatedAt:    v.CreatedAt,
		UpdatedAt:    v.UpdatedAt,
	}
}
