package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type switchCaseDef struct {
	Name     string               `json:"name"`
	Operator string               `json:"operator"`
	Right    workflow.ValueSource `json:"right"`
}

type switchConfigDef struct {
	Cases []switchCaseDef `json:"cases"`
}

type switchExecutor struct{}

func switchFactory(_ *nodetype.NodeType) executor.NodeExecutor { return switchExecutor{} }

func (switchExecutor) Execute(_ context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	var cfg switchConfigDef
	if err := json.Unmarshal(in.Config, &cfg); err != nil {
		return executor.ExecOutput{}, fmt.Errorf("switch: parse config: %w", err)
	}
	value := in.Inputs["value"]
	for _, c := range cfg.Cases {
		if c.Name == "" {
			return executor.ExecOutput{}, fmt.Errorf("switch: case name is required")
		}
		if c.Right.Kind != workflow.ValueKindLiteral {
			return executor.ExecOutput{}, fmt.Errorf("switch case %q: right.kind must be literal", c.Name)
		}
		right := c.Right.Value
		ok, err := evalCondition(c.Operator, value, right)
		if err != nil {
			return executor.ExecOutput{}, fmt.Errorf("switch case %q: %w", c.Name, err)
		}
		if ok {
			return executor.ExecOutput{Outputs: map[string]any{"matched": c.Name}, FiredPort: c.Name}, nil
		}
	}
	return executor.ExecOutput{Outputs: map[string]any{"matched": workflow.PortDefault}, FiredPort: workflow.PortDefault}, nil
}
