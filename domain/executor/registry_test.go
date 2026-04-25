package executor

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/shinya/shineflow/domain/nodetype"
)

// stubExec 是测试用的 NodeExecutor 实现，仅记录自己叫什么。
type stubExec struct{ tag string }

func (s *stubExec) Execute(_ context.Context, _ ExecInput) (ExecOutput, error) {
	return ExecOutput{Outputs: map[string]any{"tag": s.tag}}, nil
}

func newStub(tag string) ExecutorFactory {
	return func(_ *nodetype.NodeType) NodeExecutor { return &stubExec{tag: tag} }
}

func tagOf(t *testing.T, ex NodeExecutor) string {
	t.Helper()
	out, err := ex.Execute(context.Background(), ExecInput{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return out.Outputs["tag"].(string)
}

func TestRegistry_ExactWinsOverPrefix(t *testing.T) {
	r := NewRegistry()
	r.Register("builtin.llm", newStub("exact"))
	r.Register("builtin.*", newStub("wildcard"))

	ex, err := r.Build(&nodetype.NodeType{Key: "builtin.llm"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := tagOf(t, ex); got != "exact" {
		t.Errorf("got %q, want exact", got)
	}
}

func TestRegistry_LongestPrefixWins(t *testing.T) {
	r := NewRegistry()
	r.Register("plugin.*", newStub("short"))
	r.Register("plugin.http.*", newStub("long"))

	ex, err := r.Build(&nodetype.NodeType{Key: "plugin.http.hp_001"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := tagOf(t, ex); got != "long" {
		t.Errorf("got %q, want long", got)
	}
}

func TestRegistry_TwoSegmentWildcard(t *testing.T) {
	r := NewRegistry()
	r.Register("plugin.mcp.*.*", newStub("mcp"))

	ex, err := r.Build(&nodetype.NodeType{Key: "plugin.mcp.svr_1.read_file"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := tagOf(t, ex); got != "mcp" {
		t.Errorf("got %q, want mcp", got)
	}
}

func TestRegistry_SegmentCountMismatch(t *testing.T) {
	r := NewRegistry()
	r.Register("plugin.mcp.*.*", newStub("mcp"))

	// 段数不够，不应匹配
	if _, err := r.Build(&nodetype.NodeType{Key: "plugin.mcp.svr_1"}); !errors.Is(err, ErrNoExecutor) {
		t.Errorf("expected ErrNoExecutor, got %v", err)
	}
	// 段数过多，不应匹配
	if _, err := r.Build(&nodetype.NodeType{Key: "plugin.mcp.svr_1.tool.extra"}); !errors.Is(err, ErrNoExecutor) {
		t.Errorf("expected ErrNoExecutor, got %v", err)
	}
}

func TestRegistry_NoMatch(t *testing.T) {
	r := NewRegistry()
	r.Register("builtin.llm", newStub("exact"))

	if _, err := r.Build(&nodetype.NodeType{Key: "totally.unknown"}); !errors.Is(err, ErrNoExecutor) {
		t.Errorf("expected ErrNoExecutor, got %v", err)
	}
}

func TestRegistry_ExactDuplicateOverwrites(t *testing.T) {
	// 重复注册同一精确 key，后注册胜（明确该行为，避免初始化顺序歧义）
	r := NewRegistry()
	r.Register("builtin.llm", newStub("v1"))
	r.Register("builtin.llm", newStub("v2"))
	ex, err := r.Build(&nodetype.NodeType{Key: "builtin.llm"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := tagOf(t, ex); got != "v2" {
		t.Errorf("got %q, want v2", got)
	}
}

func TestRegistry_PassesNodeTypeToFactory(t *testing.T) {
	// factory 应能拿到完整 NodeType（用 reflect.DeepEqual 而不是仅看 Key）
	var captured *nodetype.NodeType
	r := NewRegistry()
	r.Register("builtin.llm", func(nt *nodetype.NodeType) NodeExecutor {
		captured = nt
		return &stubExec{tag: "x"}
	})
	nt := &nodetype.NodeType{Key: "builtin.llm", Name: "LLM", Category: nodetype.CategoryAI}
	if _, err := r.Build(nt); err != nil {
		t.Fatalf("build: %v", err)
	}
	if !reflect.DeepEqual(captured, nt) {
		t.Errorf("factory received different NodeType: %+v vs %+v", captured, nt)
	}
}
