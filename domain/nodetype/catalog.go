package nodetype

import "strings"

var builtinCatalog = []*NodeType{
	startType,
	endType,
	llmType,
	ifType,
	switchType,
	joinType,
	setVariableType,
	httpRequestType,
}

// NewBuiltinRegistry 返回预置 8 个内置 NodeType 的只进程内 registry。
func NewBuiltinRegistry() NodeTypeRegistry {
	r := newInMemoryRegistry()
	for _, nt := range builtinCatalog {
		r.put(nt)
	}
	return r
}

type inMemoryRegistry struct {
	byKey map[string]*NodeType
}

func newInMemoryRegistry() *inMemoryRegistry {
	return &inMemoryRegistry{byKey: map[string]*NodeType{}}
}

func (r *inMemoryRegistry) put(nt *NodeType) {
	r.byKey[nt.Key] = nt
}

func (r *inMemoryRegistry) Get(key string) (*NodeType, bool) {
	nt, ok := r.byKey[key]
	return nt, ok
}

func (r *inMemoryRegistry) List(filter NodeTypeFilter) []*NodeType {
	out := make([]*NodeType, 0, len(r.byKey))
	for _, nt := range r.byKey {
		if filter.Category != "" && nt.Category != filter.Category {
			continue
		}
		if filter.Builtin != nil && nt.Builtin != *filter.Builtin {
			continue
		}
		if len(filter.KeyPrefixes) > 0 && !hasAnyPrefix(nt.Key, filter.KeyPrefixes) {
			continue
		}
		out = append(out, nt)
	}
	return out
}

func (r *inMemoryRegistry) Invalidate(key string) {
	delete(r.byKey, key)
}

func (r *inMemoryRegistry) InvalidatePrefix(prefix string) {
	for k := range r.byKey {
		if strings.HasPrefix(k, prefix) {
			delete(r.byKey, k)
		}
	}
}

func hasAnyPrefix(key string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
