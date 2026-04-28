package enginetest

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/shinya/shineflow/domain/engine"
	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/executor/builtin"
	"github.com/shinya/shineflow/domain/nodetype"
)

// EngineHarness 聚合引擎单测所需的所有 fake 与 registry。
type EngineHarness struct {
	T            *testing.T
	WorkflowRepo *FakeWorkflowRepo
	RunRepo      *FakeRunRepo
	NTReg        *NodeTypeRegistry
	ExReg        executor.ExecutorRegistry
	Engine       *engine.Engine
	HTTPMock     *MockHTTPClient
	LLMMock      *MockLLMClient
}

type Option func(*EngineHarness)

func WithMockHTTPClient(m *MockHTTPClient) Option { return func(h *EngineHarness) { h.HTTPMock = m } }
func WithMockLLMClient(m *MockLLMClient) Option   { return func(h *EngineHarness) { h.LLMMock = m } }

// RegisterBuiltins 暴露给 e2e 测试复用，内部只代理 builtin.Register。
func RegisterBuiltins(reg executor.ExecutorRegistry) { builtin.Register(reg) }

// New 构造带 builtin executor 与 fake repository 的测试引擎。
func New(t *testing.T, opts ...Option) *EngineHarness {
	t.Helper()
	h := &EngineHarness{
		T:            t,
		WorkflowRepo: NewFakeWorkflowRepo(),
		RunRepo:      NewFakeRunRepo(),
		NTReg:        NewNodeTypeRegistry(),
		ExReg:        executor.NewRegistry(),
	}
	RegisterBuiltins(h.ExReg)
	for _, o := range opts {
		o(h)
	}

	services := executor.ExecServices{
		Logger:     MockLogger{},
		HTTPClient: h.HTTPMock,
		LLMClient:  h.LLMMock,
	}
	cfg := engine.Config{
		Clock:     fixedClock(),
		NewID:     newSeqID(),
		AfterFunc: instantAfter,
		RNG:       rand.New(rand.NewSource(1)),
	}
	h.Engine = engine.New(h.WorkflowRepo, h.RunRepo, h.NTReg, h.ExReg, services, cfg)
	return h
}

// NodeTypeRegistry 是测试用可写 NodeType registry。
type NodeTypeRegistry struct {
	mu    sync.Mutex
	byKey map[string]*nodetype.NodeType
}

func NewNodeTypeRegistry() *NodeTypeRegistry {
	r := &NodeTypeRegistry{byKey: map[string]*nodetype.NodeType{}}
	for _, nt := range nodetype.NewBuiltinRegistry().List(nodetype.NodeTypeFilter{}) {
		r.Put(nt)
	}
	return r
}

func (r *NodeTypeRegistry) Put(nt *nodetype.NodeType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKey[nt.Key] = cloneNodeType(nt)
}

func (r *NodeTypeRegistry) Get(key string) (*nodetype.NodeType, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	nt, ok := r.byKey[key]
	if !ok {
		return nil, false
	}
	return cloneNodeType(nt), true
}

func (r *NodeTypeRegistry) List(filter nodetype.NodeTypeFilter) []*nodetype.NodeType {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*nodetype.NodeType, 0, len(r.byKey))
	for _, nt := range r.byKey {
		if filter.Category != "" && nt.Category != filter.Category {
			continue
		}
		if filter.Builtin != nil && nt.Builtin != *filter.Builtin {
			continue
		}
		if len(filter.KeyPrefixes) > 0 {
			matched := false
			for _, prefix := range filter.KeyPrefixes {
				if len(nt.Key) >= len(prefix) && nt.Key[:len(prefix)] == prefix {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, cloneNodeType(nt))
	}
	return out
}

func (r *NodeTypeRegistry) Invalidate(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byKey, key)
}

func (r *NodeTypeRegistry) InvalidatePrefix(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.byKey {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(r.byKey, k)
		}
	}
}

func cloneNodeType(nt *nodetype.NodeType) *nodetype.NodeType {
	if nt == nil {
		return nil
	}
	clone := *nt
	clone.ConfigSchema = append(nt.ConfigSchema[:0:0], nt.ConfigSchema...)
	clone.InputSchema = append(nt.InputSchema[:0:0], nt.InputSchema...)
	clone.OutputSchema = append(nt.OutputSchema[:0:0], nt.OutputSchema...)
	clone.Ports = append(nt.Ports[:0:0], nt.Ports...)
	return &clone
}

func instantAfter(_ time.Duration, fn func()) func() {
	go fn()
	return func() {}
}

func fixedClock() func() time.Time {
	t := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

func newSeqID() func() string {
	var i int
	return func() string {
		i++
		return "id-" + itoa(i)
	}
}
