package main

import (
	"context"
	"strconv"

	"github.com/pinpt/integration-sdk/agent"

	"github.com/pinpt/agent.next/integrations/pkg/ibase"
	"github.com/pinpt/agent.next/rpcdef"

	"github.com/hashicorp/go-hclog"
)

type Integration struct {
	logger hclog.Logger
	agent  rpcdef.Agent
}

func NewIntegration(logger hclog.Logger) *Integration {
	s := &Integration{}
	s.logger = logger
	return s
}

func (s *Integration) Init(agent rpcdef.Agent) error {
	s.agent = agent
	return nil
}

func (s *Integration) Export(ctx context.Context, config rpcdef.ExportConfig) (res rpcdef.ExportResult, _ error) {
	s.exportAll()
	return res, nil
}

func (s *Integration) ValidateConfig(ctx context.Context, config rpcdef.ExportConfig) (res rpcdef.ValidationResult, _ error) {
	res.Errors = append(res.Errors, "example validation error")
	return res, nil
}

func (s *Integration) OnboardExport(ctx context.Context, objectType rpcdef.OnboardExportType, config rpcdef.ExportConfig) (res rpcdef.OnboardExportResult, _ error) {
	if objectType != rpcdef.OnboardExportTypeUsers {
		res.Error = rpcdef.ErrOnboardExportNotSupported
		return
	}

	var rows []map[string]interface{}

	for j := 0; j < 10; j++ {
		row := agent.UserResponseUsers{}
		row.Name = "User " + strconv.Itoa(j)
		rows = append(rows, row.ToMap())
	}

	res.Records = rows
	return
}

func main() {
	ibase.MainFunc(func(logger hclog.Logger) rpcdef.Integration {
		return NewIntegration(logger)
	})
}
