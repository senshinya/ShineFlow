package engine

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/run"
	"github.com/shinya/shineflow/domain/workflow"
)

// ErrVersionNotPublished 表示 Start 收到的版本处于非 release 状态。
var ErrVersionNotPublished = errors.New("workflow version is not in release state")

// TemplateMode 控制模板引用缺失时的行为。
type TemplateMode int

const (
	TemplateStrict TemplateMode = iota
	TemplateLenient
)

// Config 包含引擎可调参数和测试注入点。
type Config struct {
	Clock        func() time.Time
	NewID        func() string
	AfterFunc    func(time.Duration, func()) (stop func())
	TemplateMode TemplateMode
	RunTimeout   time.Duration
	PersistBuf   int
	RNG          *rand.Rand
}

// Engine 是无状态编排器；每次 Start 都创建独立 runState。
type Engine struct {
	workflowRepo workflow.WorkflowRepository
	runRepo      run.WorkflowRunRepository
	ntReg        nodetype.NodeTypeRegistry
	exReg        executor.ExecutorRegistry
	services     executor.ExecServices

	cfg   Config
	rngMu sync.Mutex
}

// New 构造 Engine，并为零值配置填充默认值。
func New(
	workflowRepo workflow.WorkflowRepository,
	runRepo run.WorkflowRunRepository,
	ntReg nodetype.NodeTypeRegistry,
	exReg executor.ExecutorRegistry,
	services executor.ExecServices,
	cfg Config,
) *Engine {
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.NewID == nil {
		cfg.NewID = defaultNewID
	}
	if cfg.AfterFunc == nil {
		cfg.AfterFunc = realAfterFunc
	}
	if cfg.PersistBuf <= 0 {
		cfg.PersistBuf = 64
	}
	if cfg.RNG == nil {
		cfg.RNG = rand.New(rand.NewSource(cfg.Clock().UnixNano()))
	}
	return &Engine{
		workflowRepo: workflowRepo,
		runRepo:      runRepo,
		ntReg:        ntReg,
		exReg:        exReg,
		services:     services,
		cfg:          cfg,
	}
}

// StartInput 是启动一次 Run 的请求。
type StartInput struct {
	VersionID      string
	TriggerKind    run.TriggerKind
	TriggerRef     string
	TriggerPayload []byte
	CreatedBy      string
}

// Start 驱动一次 Run 到达终态。
func (e *Engine) Start(ctx context.Context, in StartInput) (*run.WorkflowRun, error) {
	v, err := e.workflowRepo.GetVersion(ctx, in.VersionID)
	if err != nil {
		return nil, err
	}
	if v.State != workflow.VersionStateRelease {
		return nil, ErrVersionNotPublished
	}

	now := e.cfg.Clock()
	rn := &run.WorkflowRun{
		ID:             e.cfg.NewID(),
		DefinitionID:   v.DefinitionID,
		VersionID:      v.ID,
		TriggerKind:    in.TriggerKind,
		TriggerRef:     in.TriggerRef,
		TriggerPayload: in.TriggerPayload,
		Status:         run.RunStatusPending,
		CreatedBy:      in.CreatedBy,
		CreatedAt:      now,
	}
	if len(rn.TriggerPayload) == 0 {
		rn.TriggerPayload = []byte(`{}`)
	}
	if err := e.runRepo.Create(ctx, rn); err != nil {
		return nil, err
	}

	startedAt := e.cfg.Clock()
	rn.StartedAt = &startedAt
	if err := e.runRepo.UpdateStatus(ctx, rn.ID, run.RunStatusRunning, run.WithRunStartedAt(startedAt)); err != nil {
		return nil, err
	}
	rn.Status = run.RunStatusRunning

	triggers, oa := buildTriggerTable(v.DSL)
	sym, err := run.NewSymbols(rn.TriggerPayload)
	if err != nil {
		return e.finalizeFailed(ctx, rn, run.RunError{
			Code:    run.RunErrCodeTriggerInvalid,
			Message: err.Error(),
		})
	}
	st := newRunState(v.DSL, triggers, oa, sym)
	st.rng = rand.New(rand.NewSource(e.nextSeed()))

	parentCtx := ctx
	if e.cfg.RunTimeout > 0 {
		var cancelTO context.CancelFunc
		parentCtx, cancelTO = context.WithTimeout(ctx, e.cfg.RunTimeout)
		defer cancelTO()
	}
	runCtx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	persistCtx := context.WithoutCancel(ctx)
	persistCh := make(chan persistOp, e.cfg.PersistBuf)
	persistErrCh := make(chan error, 1)
	persistDone := make(chan struct{})
	go e.runPersister(persistCtx, persistCh, persistErrCh, persistDone)

	done := make(chan nodeResult, 32)
	retryCh := make(chan retryEvent, len(v.DSL.Nodes)+1)

	for nid, spec := range triggers {
		if len(spec.inEdges) == 0 {
			e.dispatch(runCtx, rn, st, nid, done, persistCh)
		}
	}

	ctxDone := runCtx.Done()
	cancelOnce := func(external bool) {
		if external {
			st.cancelled = true
		}
		cancel()
		ctxDone = nil
	}

	for st.inflight > 0 || st.pendingRetries > 0 {
		select {
		case res := <-done:
			st.inflight--
			e.handleResult(runCtx, rn, st, res, done, retryCh, persistCh)
			if st.runFail != nil {
				cancelOnce(false)
			} else if st.endHit != nil && ctxDone != nil {
				cancelOnce(false)
			}
		case rt := <-retryCh:
			st.pendingRetries--
			if rt.cancelled || st.runFail != nil || ctxDone == nil {
				e.persistRetryAborted(rn, st, rt, persistCh)
				st.nodeStat[rt.nodeID] = nodeDone
			} else {
				e.dispatch(runCtx, rn, st, rt.nodeID, done, persistCh)
			}
		case perr := <-persistErrCh:
			if st.runFail == nil {
				st.runFail = &run.RunError{Code: run.RunErrCodePersistence, Message: perr.Error()}
			}
			if ctxDone != nil {
				cancelOnce(false)
			}
		case <-ctxDone:
			cancelOnce(true)
		}
	}

	if st.endHit == nil && st.runFail == nil && runCtx.Err() != nil {
		st.cancelled = true
	}

	close(persistCh)
	<-persistDone
	select {
	case perr := <-persistErrCh:
		if st.runFail == nil {
			st.runFail = &run.RunError{Code: run.RunErrCodePersistence, Message: perr.Error()}
		}
	default:
	}

	return e.finalize(ctx, rn, st)
}

func (e *Engine) nextSeed() int64 {
	e.rngMu.Lock()
	defer e.rngMu.Unlock()
	return e.cfg.RNG.Int63()
}

func defaultNewID() string { return uuid.NewString() }

func realAfterFunc(d time.Duration, fn func()) (stop func()) {
	t := time.AfterFunc(d, fn)
	return func() { t.Stop() }
}
