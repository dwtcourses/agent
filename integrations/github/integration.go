package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/pinpt/agent2/rpcdef"
)

type Integration struct {
	logger     hclog.Logger
	agent      rpcdef.Agent
	customerID string
}

func (s *Integration) Init(agent rpcdef.Agent) error {
	s.agent = agent
	s.customerID = "c1"
	return nil
}

func (s *Integration) Export(ctx context.Context) error {
	err := s.exportRepos(ctx)
	if err != nil {
		return err
	}
	/*
		err = s.exportPullRequests(ctx)
		if err != nil {
			return err
		}*/
	return nil
}

func strInArr(str string, arr []string) bool {
	for _, v := range arr {
		if v == str {
			return true
		}
	}
	return false
}

func parseTime(ts string) int64 {
	if ts == "" {
		return 0
	}
	ts2, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		panic(err)
	}
	return ts2.Unix()
}

const batchSize = 100

func (s *Integration) makeRequest(query string, res interface{}) error {
	data := map[string]string{
		"query": query,
	}

	b, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequest("POST", "https://api.github.com/graphql", bytes.NewReader(b))
	if err != nil {
		return err
	}
	auth := os.Getenv("PP_GITHUB_TOKEN")
	if auth == "" {
		return errors.New("provide PP_GITHUB_TOKEN")
	}
	req.Header.Add("Authorization", "bearer "+auth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return errors.New(`resp resp.StatusCode != 200`)
	}

	//s.logger.Info("response body", string(b))

	err = json.Unmarshal(b, &res)
	if err != nil {
		return err
	}
	return nil
}
