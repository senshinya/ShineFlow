package executor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shinya/shineflow/domain/nodetype"
)

// ErrNoExecutor Build 找不到任何匹配的 ExecutorFactory。
var ErrNoExecutor = errors.New("executor: no matching factory")

// NewRegistry 构造一个内存版 ExecutorRegistry。
// 注册和构造都不并发安全；引擎初始化时单线程注册即可，后续 Build 也通常是同一 goroutine。
// 如果未来需要在 Build 高并发的同时支持热更新，再加 sync.RWMutex。
func NewRegistry() ExecutorRegistry { return &registry{} }

type registry struct {
	exact    map[string]ExecutorFactory
	wildcard []wildcardEntry
}

type wildcardEntry struct {
	pattern   string
	segments  []string // pattern split on "."；其中 "*" 是段通配
	factory   ExecutorFactory
	prefixLen int // pattern 中 '*' 之前的字面前缀长度（含末尾的"."）
}

func (r *registry) Register(keyPattern string, factory ExecutorFactory) {
	if !strings.Contains(keyPattern, "*") {
		if r.exact == nil {
			r.exact = map[string]ExecutorFactory{}
		}
		r.exact[keyPattern] = factory
		return
	}
	starIdx := strings.Index(keyPattern, "*")
	r.wildcard = append(r.wildcard, wildcardEntry{
		pattern:   keyPattern,
		segments:  strings.Split(keyPattern, "."),
		factory:   factory,
		prefixLen: starIdx,
	})
}

func (r *registry) Build(nt *nodetype.NodeType) (NodeExecutor, error) {
	if f, ok := r.exact[nt.Key]; ok {
		return f(nt), nil
	}
	keySegs := strings.Split(nt.Key, ".")
	var best *wildcardEntry
	for i := range r.wildcard {
		e := &r.wildcard[i]
		if !segmentMatch(e.segments, keySegs) {
			continue
		}
		if best == nil || e.prefixLen > best.prefixLen {
			best = e
		}
	}
	if best == nil {
		return nil, fmt.Errorf("%w: key %q", ErrNoExecutor, nt.Key)
	}
	return best.factory(nt), nil
}

// segmentMatch 段对段比较 pattern 与 key；"*" 匹配任意单段。段数必须相等。
func segmentMatch(pattern, key []string) bool {
	if len(pattern) != len(key) {
		return false
	}
	for i, p := range pattern {
		if p == "*" {
			continue
		}
		if p != key[i] {
			return false
		}
	}
	return true
}
