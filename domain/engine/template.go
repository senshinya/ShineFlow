package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/shinya/shineflow/domain/run"
)

var templatePattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// ExpandTemplate 按 Symbols 解析 {{path}} 模板。整串模板保留原始类型，混合字符串模板转成文本。
func ExpandTemplate(s string, sym *run.Symbols) (any, error) {
	return expandTemplate(s, sym, TemplateStrict)
}

func expandTemplate(s string, sym *run.Symbols, mode TemplateMode) (any, error) {
	if m := wholeMatch(s); m != "" {
		v, err := sym.Lookup(m)
		if err != nil {
			if mode == TemplateLenient {
				return s, nil
			}
			return nil, fmt.Errorf("template %q: %w", s, err)
		}
		return v, nil
	}
	var firstErr error
	out := templatePattern.ReplaceAllStringFunc(s, func(match string) string {
		path := strings.TrimSpace(match[2 : len(match)-2])
		v, err := sym.Lookup(path)
		if err != nil {
			if mode == TemplateLenient {
				return match
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("template %q at %s: %w", s, match, err)
			}
			return match
		}
		return formatScalar(v)
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// wholeMatch 在字符串正好是一个模板表达式时返回内部路径。
func wholeMatch(s string) string {
	loc := templatePattern.FindStringIndex(s)
	if loc == nil || loc[0] != 0 || loc[1] != len(s) {
		return ""
	}
	m := templatePattern.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// formatScalar 将值渲染为混合字符串模板可拼接的文本。
func formatScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case map[string]any, []any:
		b, _ := json.Marshal(x)
		return string(b)
	default:
		return fmt.Sprint(v)
	}
}
