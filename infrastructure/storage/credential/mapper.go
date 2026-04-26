package credential

import domaincredential "github.com/shinya/shineflow/domain/credential"

func toCredential(m *credentialModel) *domaincredential.Credential {
	return &domaincredential.Credential{
		ID:               m.ID,
		Name:             m.Name,
		Kind:             domaincredential.CredentialKind(m.Kind),
		EncryptedPayload: m.EncryptedPayload,
		CreatedBy:        m.CreatedBy,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
}

func toCredentialModel(c *domaincredential.Credential) *credentialModel {
	return &credentialModel{
		ID:               c.ID,
		Name:             c.Name,
		Kind:             string(c.Kind),
		EncryptedPayload: c.EncryptedPayload,
		CreatedBy:        c.CreatedBy,
		CreatedAt:        c.CreatedAt,
		UpdatedAt:        c.UpdatedAt,
	}
}
