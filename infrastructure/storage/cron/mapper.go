package cron

import (
	"encoding/json"

	domaincron "github.com/shinya/shineflow/domain/cron"
)

var emptyJSON = json.RawMessage("{}")

func toCronJob(m *cronJobModel) *domaincron.CronJob {
	return &domaincron.CronJob{
		ID:           m.ID,
		DefinitionID: m.DefinitionID,
		Name:         m.Name,
		Description:  m.Description,
		Expression:   m.Expression,
		Timezone:     m.Timezone,
		Payload:      m.Payload,
		Enabled:      m.Enabled,
		NextFireAt:   m.NextFireAt,
		LastFireAt:   m.LastFireAt,
		LastRunID:    m.LastRunID,
		CreatedBy:    m.CreatedBy,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

func toCronJobModel(j *domaincron.CronJob) *cronJobModel {
	payload := j.Payload
	if len(payload) == 0 {
		payload = emptyJSON
	}
	return &cronJobModel{
		ID:           j.ID,
		DefinitionID: j.DefinitionID,
		Name:         j.Name,
		Description:  j.Description,
		Expression:   j.Expression,
		Timezone:     j.Timezone,
		Payload:      payload,
		Enabled:      j.Enabled,
		NextFireAt:   j.NextFireAt,
		LastFireAt:   j.LastFireAt,
		LastRunID:    j.LastRunID,
		CreatedBy:    j.CreatedBy,
		CreatedAt:    j.CreatedAt,
		UpdatedAt:    j.UpdatedAt,
	}
}
