// Package exporter for scheduling and executing exports as part of service-run
package exporter

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/pinpt/agent.next/cmd/cmdintegration"
	"github.com/pinpt/agent.next/cmd/cmdservicerunnorestarts/inconfig"
	"github.com/pinpt/agent.next/cmd/cmdservicerunnorestarts/subcommand"
	"github.com/pinpt/agent.next/cmd/cmdupload"
	"github.com/pinpt/go-common/event"

	"github.com/pinpt/agent.next/pkg/agentconf"
	"github.com/pinpt/agent.next/pkg/date"
	"github.com/pinpt/agent.next/pkg/deviceinfo"
	"github.com/pinpt/agent.next/pkg/fsconf"
	"github.com/pinpt/agent.next/pkg/jsonstore"
	"github.com/pinpt/agent.next/pkg/logutils"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/pinpt/agent.next/cmd/cmdexport"
	"github.com/pinpt/integration-sdk/agent"
)

// Opts are the options for Exporter
type Opts struct {
	Logger hclog.Logger
	// LogLevelSubcommands specifies the log level to pass to sub commands.
	// Pass the same as used for logger.
	// We need it here, because there is no way to get it from logger.
	LogLevelSubcommands hclog.Level

	PinpointRoot string
	FSConf       fsconf.Locs
	Conf         agentconf.Config

	PPEncryptionKey string
	AgentConfig     cmdintegration.AgentConfig

	IntegrationsDir string
}

// Exporter schedules and executes exports
type Exporter struct {
	// ExportQueue for queuing the exports
	// Exports happen serially, with only one happening at once
	ExportQueue chan Request

	conf agentconf.Config

	logger     hclog.Logger
	opts       Opts
	mu         sync.Mutex
	exporting  bool
	deviceInfo deviceinfo.CommonInfo
}

// Request is the export request to put into the ExportQueue
type Request struct {
	// Done is the callback for when the export is completed
	Done chan bool
	// Data is the ExportRequest data received from the server
	Data *agent.ExportRequest
	// MessageID is the message id received from the server in headers
	MessageID string
}

// New creates exporter
func New(opts Opts) (*Exporter, error) {
	if opts.PPEncryptionKey == "" {
		return nil, errors.New(`opts.PPEncryptionKey == ""`)
	}
	s := &Exporter{}
	s.opts = opts
	s.conf = opts.Conf
	s.deviceInfo = deviceinfo.CommonInfo{
		CustomerID: s.conf.CustomerID,
		SystemID:   s.conf.SystemID,
		DeviceID:   s.conf.DeviceID,
		Root:       s.opts.PinpointRoot,
	}
	s.logger = opts.Logger
	s.ExportQueue = make(chan Request)
	return s, nil
}

// Run starts processing ExportQueue. This is a blocking call.
func (s *Exporter) Run() {
	for req := range s.ExportQueue {
		s.setRunning(true)
		s.export(req.Data, req.MessageID)
		s.setRunning(false)
		req.Done <- true
	}
	return
}

func (s *Exporter) setRunning(ex bool) {
	s.mu.Lock()
	s.exporting = ex
	s.mu.Unlock()
}

// IsRunning returns true if there is an export in progress
func (s *Exporter) IsRunning() bool {
	s.mu.Lock()
	ex := s.exporting
	s.mu.Unlock()
	return ex
}
func (s *Exporter) sendExportEvent(ctx context.Context, jobID string, data *agent.ExportResponse, ints []agent.ExportRequestIntegrations, isIncremental []bool) error {
	data.JobID = jobID
	data.RefType = "export"
	data.Type = agent.ExportResponseTypeExport
	for i, in := range ints {
		v := agent.ExportResponseIntegrations{
			IntegrationID: in.ID,
			Name:          in.Name,
			SystemType:    agent.ExportResponseIntegrationsSystemType(in.SystemType),
		}
		if len(isIncremental) != 0 { // only sending this for completed event
			if len(isIncremental) <= i {
				return errors.New("could not check if export was incremental or not, isIncremental array is not of valid length")
			}
			if isIncremental[i] {
				v.ExportType = agent.ExportResponseIntegrationsExportTypeIncremental
			} else {
				v.ExportType = agent.ExportResponseIntegrationsExportTypeHistorical
			}
		}
		data.Integrations = append(data.Integrations, v)
	}
	s.deviceInfo.AppendCommonInfo(data)
	publishEvent := event.PublishEvent{
		Object: data,
		Headers: map[string]string{
			"uuid": s.conf.DeviceID,
		},
	}
	return event.Publish(ctx, publishEvent, s.conf.Channel, s.conf.APIKey)
}

func (s *Exporter) sendStartExportEvent(ctx context.Context, jobID string, ints []agent.ExportRequestIntegrations) error {
	data := &agent.ExportResponse{
		State:   agent.ExportResponseStateStarting,
		Success: true,
	}
	return s.sendExportEvent(ctx, jobID, data, ints, nil)
}

func (s *Exporter) sendEndExportEvent(ctx context.Context, jobID string, started, ended time.Time, partsCount int, filesize int64, uploadurl *string, ints []agent.ExportRequestIntegrations, isIncremental []bool, err error) error {
	if !s.opts.AgentConfig.Backend.Enable {
		return nil
	}
	data := &agent.ExportResponse{
		State:           agent.ExportResponseStateCompleted,
		Size:            filesize,
		UploadURL:       uploadurl,
		UploadPartCount: int64(partsCount),
	}
	date.ConvertToModel(started, &data.StartDate)
	date.ConvertToModel(ended, &data.EndDate)
	if err != nil {
		errstr := err.Error()
		data.Error = &errstr
		data.Success = false
	} else {
		data.Success = true
	}
	return s.sendExportEvent(ctx, jobID, data, ints, isIncremental)
}

func (s *Exporter) export(data *agent.ExportRequest, messageID string) {
	ctx := context.Background()
	if len(data.Integrations) == 0 {
		s.logger.Error("passed export request has no integrations, ignoring it")
		return
	}

	started := time.Now()
	if err := s.sendStartExportEvent(ctx, data.JobID, data.Integrations); err != nil {
		s.logger.Error("error sending export response start event", "err", err)
	}
	isIncremental, partsCount, fileSize, err := s.doExport(ctx, data, messageID)
	if err != nil {
		s.logger.Error("export finished with error", "err", err)
	} else {
		s.logger.Info("sent back export result")
	}
	if err := s.sendEndExportEvent(ctx, data.JobID, started, time.Now(), partsCount, fileSize, data.UploadURL, data.Integrations, isIncremental, err); err != nil {
		s.logger.Error("error sending export response stop event", "err", err)
	}
}

func (s *Exporter) doExport(ctx context.Context, data *agent.ExportRequest, messageID string) (isIncremental []bool, partsCount int, fileSize int64, rerr error) {
	s.logger.Info("processing export request", "job_id", data.JobID, "request_date", data.RequestDate.Rfc3339, "reprocess_historical", data.ReprocessHistorical)

	var integrations []cmdexport.Integration
	// add in additional integrations defined in config
	for _, in := range s.conf.ExtraIntegrations {
		integrations = append(integrations, cmdexport.Integration{
			Name:   in.Name,
			Config: in.Config,
		})
	}

	lastProcessedStore, err := jsonstore.New(s.opts.FSConf.LastProcessedFile)
	if err != nil {
		rerr = err
		return
	}

	for _, integration := range data.Integrations {
		s.logger.Info("exporting integration", "name", integration.Name, "len(exclusions)", len(integration.Exclusions))
		conf, err := inconfig.ConfigFromEvent(integration.ToMap(), inconfig.IntegrationType(integration.SystemType), s.opts.PPEncryptionKey)
		if err != nil {
			rerr = err
			return
		}
		integrations = append(integrations, conf)

		if data.ReprocessHistorical {
			isIncremental = append(isIncremental, false)
		} else {
			lastProcessed, err := s.getLastProcessed(lastProcessedStore, conf)
			if err != nil {
				rerr = err
				return
			}
			isIncremental = append(isIncremental, lastProcessed != "")
		}
	}

	fsconf := s.opts.FSConf
	// delete existing uploads
	if err = os.RemoveAll(fsconf.Uploads); err != nil {
		rerr = err
		return
	}

	if err := s.execExport(ctx, integrations, data.ReprocessHistorical, messageID, data.JobID); err != nil {
		rerr = err
		return
	}
	s.logger.Info("export finished, running upload")
	if partsCount, fileSize, err = cmdupload.Run(ctx, s.logger, s.opts.PinpointRoot, *data.UploadURL, s.conf.APIKey); err != nil {
		if err == cmdupload.ErrNoFilesFound {
			s.logger.Info("skipping upload, no files generated")
			// do not return errors when no files to upload, which is ok for incremental
		} else {
			rerr = err
			return
		}
	}
	return
}

func (s *Exporter) getLastProcessed(lastProcessed *jsonstore.Store, in cmdexport.Integration) (string, error) {
	id, err := in.ID()
	if err != nil {
		return "", err
	}
	v := lastProcessed.Get(id.String())
	if v == nil {
		return "", nil
	}
	ts, ok := v.(string)
	if !ok {
		return "", errors.New("not a valid value saved in last processed key")
	}
	return ts, nil
}

func (s *Exporter) execExport(ctx context.Context, integrations []cmdexport.Integration, reprocessHistorical bool, messageID string, jobID string) error {

	agentConfig := s.opts.AgentConfig
	agentConfig.Backend.ExportJobID = jobID

	c, err := subcommand.New(subcommand.Opts{
		Logger:            s.logger,
		Tmpdir:            s.opts.FSConf.Temp,
		IntegrationConfig: agentConfig,
		AgentConfig:       s.conf,
		Integrations:      integrations,
		DeviceInfo:        s.deviceInfo,
	})
	if err != nil {
		return err
	}
	args := []string{
		"--log-level", logutils.LogLevelToString(s.opts.LogLevelSubcommands),
	}
	if reprocessHistorical {
		args = append(args, "--reprocess-historical=true")
	}
	err = c.Run(ctx, "export", messageID, nil, args...)
	if err != nil {
		return err
	}
	return err
}
