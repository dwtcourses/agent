package main

import (
	"context"

	"github.com/pinpt/agent/integrations/jira-hosted/api"
	"github.com/pinpt/agent/integrations/jira/jiracommon"
	"github.com/pinpt/agent/rpcdef"
)

func (s *Integration) OnboardExport(ctx context.Context, objectType rpcdef.OnboardExportType, config rpcdef.ExportConfig) (res rpcdef.OnboardExportResult, _ error) {
	switch objectType {
	case rpcdef.OnboardExportTypeProjects:
		return s.onboardExportProjects(ctx, config)
	case rpcdef.OnboardExportTypeWorkConfig:
		return s.onboardWorkConfig(ctx, config)
	default:
		res.Error = rpcdef.ErrOnboardExportNotSupported
		return
	}
}

func (s *Integration) onboardExportProjects(ctx context.Context, config rpcdef.ExportConfig) (res rpcdef.OnboardExportResult, rerr error) {
	err := s.initWithConfig(config, false)
	if err != nil {
		rerr = err
		return
	}
	projects, err := api.ProjectsOnboard(s.qc)
	if err != nil {
		rerr = err
		return
	}

	var records []map[string]interface{}
	for _, obj := range projects {
		records = append(records, obj.ToMap())
	}
	res.Data = records
	return res, nil
}

func (s *Integration) onboardWorkConfig(ctx context.Context, config rpcdef.ExportConfig) (res rpcdef.OnboardExportResult, _ error) {

	err := s.initWithConfig(config, false)
	if err != nil {
		return res, err
	}

	return jiracommon.GetWorkConfig(s.qc.Common())
}
