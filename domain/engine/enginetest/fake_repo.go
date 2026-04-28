package enginetest

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

// FakeWorkflowRepo 是内存版 WorkflowRepository。
type FakeWorkflowRepo struct {
	mu          sync.Mutex
	defs        map[string]*workflow.WorkflowDefinition
	versions    map[string]*workflow.WorkflowVersion
	byDef       map[string][]*workflow.WorkflowVersion
	nextVersion int
}

func NewFakeWorkflowRepo() *FakeWorkflowRepo {
	return &FakeWorkflowRepo{
		defs:     map[string]*workflow.WorkflowDefinition{},
		versions: map[string]*workflow.WorkflowVersion{},
		byDef:    map[string][]*workflow.WorkflowVersion{},
	}
}

func (f *FakeWorkflowRepo) PutVersion(v *workflow.WorkflowVersion) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.versions[v.ID] = v
	f.byDef[v.DefinitionID] = append(f.byDef[v.DefinitionID], v)
}

func (f *FakeWorkflowRepo) CreateDefinition(_ context.Context, def *workflow.WorkflowDefinition) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defs[def.ID] = def
	return nil
}

func (f *FakeWorkflowRepo) GetDefinition(_ context.Context, id string) (*workflow.WorkflowDefinition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	def, ok := f.defs[id]
	if !ok {
		return nil, workflow.ErrDefinitionNotFound
	}
	return def, nil
}

func (f *FakeWorkflowRepo) ListDefinitions(_ context.Context, _ workflow.DefinitionFilter) ([]*workflow.WorkflowDefinition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*workflow.WorkflowDefinition, 0, len(f.defs))
	for _, def := range f.defs {
		out = append(out, def)
	}
	return out, nil
}

func (f *FakeWorkflowRepo) UpdateDefinition(_ context.Context, def *workflow.WorkflowDefinition) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.defs[def.ID]; !ok {
		return workflow.ErrDefinitionNotFound
	}
	f.defs[def.ID] = def
	return nil
}

func (f *FakeWorkflowRepo) DeleteDefinition(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.defs[id]; !ok {
		return workflow.ErrDefinitionNotFound
	}
	delete(f.defs, id)
	return nil
}

func (f *FakeWorkflowRepo) GetVersion(_ context.Context, id string) (*workflow.WorkflowVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.versions[id]
	if !ok {
		return nil, workflow.ErrVersionNotFound
	}
	return v, nil
}

func (f *FakeWorkflowRepo) ListVersions(_ context.Context, definitionID string) ([]*workflow.WorkflowVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*workflow.WorkflowVersion, len(f.byDef[definitionID]))
	copy(out, f.byDef[definitionID])
	return out, nil
}

func (f *FakeWorkflowRepo) SaveVersion(_ context.Context, definitionID string, dsl workflow.WorkflowDSL, _ int) (*workflow.WorkflowVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextVersion++
	v := &workflow.WorkflowVersion{
		ID:           "version-" + itoa(f.nextVersion),
		DefinitionID: definitionID,
		Version:      f.nextVersion,
		State:        workflow.VersionStateDraft,
		DSL:          dsl,
		Revision:     1,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	f.versions[v.ID] = v
	f.byDef[definitionID] = append(f.byDef[definitionID], v)
	return v, nil
}

func (f *FakeWorkflowRepo) PublishVersion(_ context.Context, versionID, publishedBy string) (*workflow.WorkflowVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.versions[versionID]
	if !ok {
		return nil, workflow.ErrVersionNotFound
	}
	now := time.Now().UTC()
	v.State = workflow.VersionStateRelease
	v.PublishedAt = &now
	v.PublishedBy = &publishedBy
	return v, nil
}

func (f *FakeWorkflowRepo) DiscardDraft(_ context.Context, definitionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.byDef[definitionID][:0]
	for _, v := range f.byDef[definitionID] {
		if v.State == workflow.VersionStateDraft {
			delete(f.versions, v.ID)
			continue
		}
		kept = append(kept, v)
	}
	f.byDef[definitionID] = kept
	return nil
}

// FakeRunRepo 是内存版 WorkflowRunRepository。
type FakeRunRepo struct {
	mu       sync.Mutex
	runs     map[string]*run.WorkflowRun
	nodeRuns map[string][]*run.NodeRun
}

func NewFakeRunRepo() *FakeRunRepo {
	return &FakeRunRepo{runs: map[string]*run.WorkflowRun{}, nodeRuns: map[string][]*run.NodeRun{}}
}

func (f *FakeRunRepo) Create(_ context.Context, rn *run.WorkflowRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs[rn.ID] = rn
	return nil
}

func (f *FakeRunRepo) Get(_ context.Context, id string) (*run.WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rn, ok := f.runs[id]
	if !ok {
		return nil, run.ErrRunNotFound
	}
	return rn, nil
}

func (f *FakeRunRepo) List(_ context.Context, _ run.RunFilter) ([]*run.WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*run.WorkflowRun, 0, len(f.runs))
	for _, rn := range f.runs {
		out = append(out, rn)
	}
	return out, nil
}

func (f *FakeRunRepo) UpdateStatus(_ context.Context, id string, status run.RunStatus, opts ...run.RunUpdateOpt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rn, ok := f.runs[id]
	if !ok {
		return run.ErrRunNotFound
	}
	rn.Status = status
	upd := run.RunUpdate{}
	for _, o := range opts {
		o(&upd)
	}
	if upd.StartedAt != nil {
		rn.StartedAt = upd.StartedAt
	}
	if upd.EndedAt != nil {
		rn.EndedAt = upd.EndedAt
	}
	return nil
}

func (f *FakeRunRepo) SaveEndResult(_ context.Context, id, endNodeID string, output json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rn, ok := f.runs[id]
	if !ok {
		return run.ErrRunNotFound
	}
	rn.EndNodeID = &endNodeID
	rn.Output = output
	return nil
}

func (f *FakeRunRepo) SaveVars(_ context.Context, id string, vars json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rn, ok := f.runs[id]
	if !ok {
		return run.ErrRunNotFound
	}
	rn.Vars = vars
	return nil
}

func (f *FakeRunRepo) SaveError(_ context.Context, id string, e run.RunError) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rn, ok := f.runs[id]
	if !ok {
		return run.ErrRunNotFound
	}
	rn.Error = &e
	return nil
}

func (f *FakeRunRepo) AppendNodeRun(_ context.Context, runID string, nr *run.NodeRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.runs[runID]; !ok {
		return run.ErrRunNotFound
	}
	nr.RunID = runID
	f.nodeRuns[runID] = append(f.nodeRuns[runID], nr)
	return nil
}

func (f *FakeRunRepo) UpdateNodeRunStatus(_ context.Context, runID, nodeRunID string, status run.NodeRunStatus, opts ...run.NodeRunUpdateOpt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, nr := range f.nodeRuns[runID] {
		if nr.ID == nodeRunID {
			nr.Status = status
			upd := run.NodeRunUpdate{}
			for _, o := range opts {
				o(&upd)
			}
			if upd.StartedAt != nil {
				nr.StartedAt = upd.StartedAt
			}
			if upd.EndedAt != nil {
				nr.EndedAt = upd.EndedAt
			}
			if upd.Error != nil {
				nr.Error = upd.Error
			}
			if upd.FallbackApplied != nil {
				nr.FallbackApplied = *upd.FallbackApplied
			}
			if upd.ExternalRefs != nil {
				nr.ExternalRefs = upd.ExternalRefs
			}
			return nil
		}
	}
	return run.ErrNodeRunNotFound
}

func (f *FakeRunRepo) SaveNodeRunResolved(_ context.Context, runID, nodeRunID string, resolvedConfig, resolvedInputs json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, nr := range f.nodeRuns[runID] {
		if nr.ID == nodeRunID {
			nr.ResolvedConfig = resolvedConfig
			nr.ResolvedInputs = resolvedInputs
			return nil
		}
	}
	return run.ErrNodeRunNotFound
}

func (f *FakeRunRepo) SaveNodeRunOutput(_ context.Context, runID, nodeRunID string, output json.RawMessage, firedPort string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, nr := range f.nodeRuns[runID] {
		if nr.ID == nodeRunID {
			nr.Output = output
			nr.FiredPort = firedPort
			return nil
		}
	}
	return run.ErrNodeRunNotFound
}

func (f *FakeRunRepo) GetNodeRun(_ context.Context, runID, nodeRunID string) (*run.NodeRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, nr := range f.nodeRuns[runID] {
		if nr.ID == nodeRunID {
			return nr, nil
		}
	}
	return nil, run.ErrNodeRunNotFound
}

func (f *FakeRunRepo) ListNodeRuns(_ context.Context, runID string) ([]*run.NodeRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*run.NodeRun, len(f.nodeRuns[runID]))
	copy(out, f.nodeRuns[runID])
	return out, nil
}

func (f *FakeRunRepo) GetLatestNodeRun(_ context.Context, runID, nodeID string) (*run.NodeRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest *run.NodeRun
	for _, nr := range f.nodeRuns[runID] {
		if nr.NodeID == nodeID {
			latest = nr
		}
	}
	if latest == nil {
		return nil, run.ErrNodeRunNotFound
	}
	return latest, nil
}
