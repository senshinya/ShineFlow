package engine

import (
	"encoding/json"
	"math/rand"

	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

type readiness int

const (
	notReady readiness = iota
	readyToRun
	readyToSkip
)

type edgeState int

const (
	edgePending edgeState = iota
	edgeLive
	edgeDead
)

type nodeStatus int

const (
	nodeUnready nodeStatus = iota
	nodeRunning
	nodeDone
)

type joinMode int

const (
	joinAny joinMode = iota
	joinAll
)

// triggerSpec 是单个节点在本次运行中的静态触发规格。
type triggerSpec struct {
	nodeID  string
	inEdges []inEdgeRef
	mode    joinMode
}

type inEdgeRef struct {
	EdgeID     string
	SourceNode string
	SourcePort string
}

type triggerTable map[string]*triggerSpec
type outAdj map[string][]workflow.Edge

// nodeResult 是 worker 发送给 driver 的执行结果。
type nodeResult struct {
	nodeID          string
	nodeRunID       string
	attempt         int
	output          map[string]any
	resolvedInputs  json.RawMessage
	resolvedConfig  json.RawMessage
	firedPort       string
	externalRefs    []run.ExternalRef
	err             error
	fallbackApplied bool
}

// runState 只由 driver 持有和修改，因此无需加锁。
type runState struct {
	dsl      workflow.WorkflowDSL
	byID     map[string]*workflow.Node
	triggers triggerTable
	outAdj   outAdj
	sym      *run.Symbols
	rng      *rand.Rand

	edgeState map[string]edgeState
	nodeStat  map[string]nodeStatus

	attemptCounter map[string]int

	inflight       int
	pendingRetries int

	endHit    *string
	runFail   *run.RunError
	cancelled bool
}

func newRunState(dsl workflow.WorkflowDSL, t triggerTable, oa outAdj, sym *run.Symbols) *runState {
	byID := make(map[string]*workflow.Node, len(dsl.Nodes))
	for i := range dsl.Nodes {
		n := &dsl.Nodes[i]
		byID[n.ID] = n
	}
	return &runState{
		dsl:            dsl,
		byID:           byID,
		triggers:       t,
		outAdj:         oa,
		sym:            sym,
		edgeState:      make(map[string]edgeState, len(dsl.Edges)),
		nodeStat:       make(map[string]nodeStatus, len(dsl.Nodes)),
		attemptCounter: map[string]int{},
	}
}
