package main

import (
	"context"
	"fmt"

	"github.com/pinpt/agent.next/pkg/structmarshal"

	"github.com/pinpt/agent.next/integrations/pkg/jiracommon"

	"github.com/hashicorp/go-hclog"
	"github.com/pinpt/agent.next/integrations/jira-hosted/api"
	"github.com/pinpt/agent.next/integrations/pkg/ibase"
	"github.com/pinpt/agent.next/pkg/objsender"
	"github.com/pinpt/agent.next/rpcdef"
	"github.com/pinpt/integration-sdk/work"
)

func main() {
	ibase.MainFunc(func(logger hclog.Logger) rpcdef.Integration {
		return NewIntegration(logger)
	})
}

type Integration struct {
	logger hclog.Logger
	agent  rpcdef.Agent
	config Config
	qc     api.QueryContext

	common *jiracommon.JiraCommon
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

type Config struct {
	URL              string   `json:"url"`
	Username         string   `json:"username"`
	Password         string   `json:"password"`
	ExcludedProjects []string `json:"excluded_projects"`
}

func (s *Integration) setIntegrationConfig(data map[string]interface{}) error {
	rerr := func(msg string, args ...interface{}) error {
		return fmt.Errorf("config validation error: "+msg, args...)
	}
	var conf Config
	err := structmarshal.MapToStruct(data, &conf)
	if err != nil {
		return err
	}
	if conf.URL == "" {
		return rerr("url is missing")
	}
	if conf.Username == "" {
		return rerr("username is missing")
	}
	if conf.Password == "" {
		return rerr("password is missing")
	}
	s.config = conf
	return nil
}

func (s *Integration) Export(ctx context.Context, config rpcdef.ExportConfig) (res rpcdef.ExportResult, _ error) {
	err := s.setIntegrationConfig(config.Integration)
	if err != nil {
		return res, err
	}

	s.qc.CustomerID = config.Pinpoint.CustomerID
	s.qc.Logger = s.logger

	{
		opts := api.RequesterOpts{}
		opts.Logger = s.logger
		opts.APIURL = s.config.URL
		opts.Username = s.config.Username
		opts.Password = s.config.Password
		requester := api.NewRequester(opts)

		s.qc.Request = requester.Request
	}

	s.common, err = jiracommon.New(jiracommon.Opts{
		Logger:     s.logger,
		CustomerID: config.Pinpoint.CustomerID,
		Request:    s.qc.Request,
		Agent:      s.agent,
	})
	if err != nil {
		return res, err
	}
	defer s.common.ExportDone()

	fields, err := s.fields()
	if err != nil {
		return res, err
	}

	fieldByID := map[string]*work.CustomField{}
	for _, f := range fields {
		fieldByID[f.RefID] = f
	}

	projects, err := s.projects(s.config.ExcludedProjects)
	if err != nil {
		return res, err
	}

	err = s.common.IssuesAndChangelogs(projects, fieldByID)
	if err != nil {
		return res, err
	}

	return res, nil
}

type Project = jiracommon.Project

func (s *Integration) projects(excluded []string) (all []Project, _ error) {
	sender := objsender.NewNotIncremental(s.agent, "work.project")
	defer sender.Done()

	res, err := api.Projects(s.qc)
	if err != nil {
		return nil, err
	}

	excludedMap := map[string]bool{}
	for _, id := range excluded {
		excludedMap[id] = true
	}

	{
		var filtered []*work.Project
		for _, obj := range res {
			if excludedMap[obj.RefID] {
				continue
			}
			filtered = append(filtered, obj)
		}
		res = filtered
	}

	for _, obj := range res {
		p := Project{}
		p.JiraID = obj.RefID
		p.Key = obj.Identifier
		all = append(all, p)
	}

	var res2 []objsender.Model
	for _, obj := range res {
		res2 = append(res2, obj)
	}
	err = sender.Send(res2)

	if err != nil {
		return nil, err
	}

	return
}

func (s *Integration) fields() ([]*work.CustomField, error) {
	sender := objsender.NewNotIncremental(s.agent, "work.custom_field")
	defer sender.Done()

	res, err := api.FieldsAll(s.qc)
	if err != nil {
		return nil, err
	}
	var res2 []objsender.Model
	for _, item := range res {
		res2 = append(res2, item)
	}
	return res, sender.Send(res2)
}
