package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
)

type ifConfig struct {
	Operator string `json:"operator"`
}

type ifExecutor struct{}

func ifFactory(_ *nodetype.NodeType) executor.NodeExecutor { return ifExecutor{} }

func (ifExecutor) Execute(_ context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	var cfg ifConfig
	if err := json.Unmarshal(in.Config, &cfg); err != nil {
		return executor.ExecOutput{}, fmt.Errorf("if: parse config: %w", err)
	}
	result, err := evalCondition(cfg.Operator, in.Inputs["left"], in.Inputs["right"])
	if err != nil {
		return executor.ExecOutput{}, err
	}
	port := nodetype.PortIfFalse
	if result {
		port = nodetype.PortIfTrue
	}
	return executor.ExecOutput{Outputs: map[string]any{"result": result}, FiredPort: port}, nil
}

// evalCondition 计算 if/switch 共用的条件表达式。
func evalCondition(op string, left, right any) (bool, error) {
	switch op {
	case "is_empty":
		return isEmpty(left), nil
	case "is_not_empty":
		return !isEmpty(left), nil
	case "eq":
		return compareEqual(left, right), nil
	case "ne":
		return !compareEqual(left, right), nil
	case "gt", "lt", "gte", "lte":
		lf, lok := coerceFloat64(left)
		rf, rok := coerceFloat64(right)
		if !lok || !rok {
			return false, fmt.Errorf("operator %q: left/right type mismatch (need numeric)", op)
		}
		switch op {
		case "gt":
			return lf > rf, nil
		case "lt":
			return lf < rf, nil
		case "gte":
			return lf >= rf, nil
		case "lte":
			return lf <= rf, nil
		}
	case "contains":
		ls, lok := left.(string)
		rs, rok := right.(string)
		if !lok || !rok {
			return false, fmt.Errorf("operator contains: both sides must be string")
		}
		return strings.Contains(ls, rs), nil
	case "starts_with":
		ls, lok := left.(string)
		rs, rok := right.(string)
		if !lok || !rok {
			return false, fmt.Errorf("operator starts_with: both sides must be string")
		}
		return strings.HasPrefix(ls, rs), nil
	}
	return false, fmt.Errorf("unknown operator: %q", op)
}

func compareEqual(left, right any) bool {
	if lf, lok := coerceFloat64(left); lok {
		if rf, rok := coerceFloat64(right); rok {
			return lf == rf
		}
	}
	return reflect.DeepEqual(left, right)
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}
