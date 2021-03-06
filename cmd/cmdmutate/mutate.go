package cmdmutate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pinpt/agent/rpcdef"

	"github.com/pinpt/agent/cmd/cmdintegration"
)

type Mutation struct {
	// Fn is the name of the mutation function
	Fn string `json:"fn"`
	// Data contains mutation parameters as json
	Data interface{} `json:"data"`
}

type Result struct {
	MutatedObjects rpcdef.MutatedObjects `json:"mutated_objects"`
	WebappResponse interface{}           `json:"webapp_response"`
	Success        bool                  `json:"success"`
	Error          string                `json:"error"`
	ErrorCode      string                `json:"error_code"`
}

type Opts struct {
	cmdintegration.Opts
	Output   io.Writer
	Mutation Mutation
}

func Run(opts Opts) error {
	exp, err := newExport(opts)
	if err != nil {
		return err
	}
	return exp.Destroy()
}

type export struct {
	*cmdintegration.Command

	Opts Opts

	integration cmdintegration.Integration
}

func newExport(opts Opts) (*export, error) {
	s := &export{}
	if len(opts.Integrations) != 1 {
		panic("pass exactly 1 integration")
	}

	var err error
	s.Command, err = cmdintegration.NewCommand(opts.Opts)
	if err != nil {
		return nil, err
	}
	s.Opts = opts

	err = s.SetupIntegrations(nil)
	if err != nil {
		return nil, err
	}

	s.integration = s.OnlyIntegration()

	err = s.runAndPrint()
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *export) runAndPrint() error {
	res0, err := s.run()

	res := Result{}
	if err != nil {
		res.Error = err.Error()
	} else if res0.ErrorCode != "" {
		res.ErrorCode = res0.ErrorCode
		res.Error = res0.Error
		if res.Error == "" {
			res.Error = "Full error message not provided for status code: " + res.ErrorCode + ". This is a bug, we should always provide full error message."
		}
	} else if res0.Error != "" {
		res.Error = res0.Error
	} else {
		res.Success = true
		res.MutatedObjects = res0.MutatedObjects
		res.WebappResponse = res0.WebappResponse
	}
	// add more context
	if res.Error != "" {
		res.Error = fmt.Sprintf("%v (%v/%v)", res.Error, s.integration.Export.IntegrationDef.Name, strings.ToLower(s.Opts.Mutation.Fn))
	}

	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	_, err = s.Opts.Output.Write(b)
	if err != nil {
		return err
	}

	s.Logger.Info("mutate completed", "success", res.Success, "err", res.Error)

	// BUG: last log message is missing without this
	time.Sleep(10 * time.Millisecond)
	return nil
}

func (s *export) run() (_ rpcdef.MutateResult, rerr error) {
	ctx := context.Background()
	client := s.integration.ILoader.RPCClient()

	data, err := json.Marshal(s.Opts.Mutation.Data)
	if err != nil {
		rerr = err
		return
	}
	res, err := client.Mutate(ctx, s.Opts.Mutation.Fn, string(data), s.integration.ExportConfig)
	if err != nil {
		_ = s.CloseOnlyIntegrationAndHandlePanic(s.integration.ILoader)
		rerr = err
		return
	}
	err = s.CloseOnlyIntegrationAndHandlePanic(s.integration.ILoader)
	if err != nil {
		rerr = fmt.Errorf("error closing integration, err: %v", err)
		return
	}
	return res, nil
}

func (s *export) Destroy() error {
	return nil
}
