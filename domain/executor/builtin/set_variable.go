package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/nodetype"
	"github.com/shinya/shineflow/domain/workflow"
)

type setVariableConfig struct {
	Name string `json:"name"`
}

type setVariableExecutor struct{}

func setVariableFactory(_ *nodetype.NodeType) executor.NodeExecutor { return setVariableExecutor{} }

func (setVariableExecutor) Execute(_ context.Context, in executor.ExecInput) (executor.ExecOutput, error) {
	var cfg setVariableConfig
	if err := json.Unmarshal(in.Config, &cfg); err != nil {
		return executor.ExecOutput{}, fmt.Errorf("set_variable: parse config: %w", err)
	}
	if cfg.Name == "" {
		return executor.ExecOutput{}, fmt.Errorf("set_variable: config.name is required")
	}
	value, ok := in.Inputs["value"]
	if !ok {
		return executor.ExecOutput{}, fmt.Errorf("set_variable: input.value is required")
	}
	return executor.ExecOutput{
		Outputs:   map[string]any{cfg.Name: value},
		FiredPort: workflow.PortDefault,
	}, nil
}
