package requests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/go-hclog"
)

type Request struct {
	Method string
	URL    string
	// Query defines additional query parameters
	// If url already has some and Query is defined, they will be added together
	Query  url.Values
	Header http.Header

	// Body is sent to the server.
	Body []byte

	// BodyReader is sent to the server.
	// Use for large files.
	// Need to be careful for concurrent use.
	// When Request is called seeks to beginning.
	BodyReader io.ReadSeeker

	BasicAuthUser     string
	BasicAuthPassword string
}

func NewRequest() Request {
	return Request{
		Query:  url.Values{},
		Header: http.Header{},
	}
}

func (s Request) Request() (*http.Request, error) {
	m := s.Method
	if m == "" {
		m = "GET"
	}
	u, err := url.Parse(s.URL)
	if err != nil {
		return nil, err
	}
	if len(s.Query) != 0 {
		q := u.Query()
		for k, vv := range s.Query {
			for _, v := range vv {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
	}

	var req *http.Request
	if s.BodyReader == nil {
		req, err = http.NewRequest(m, u.String(), bytes.NewReader(s.Body))
		if err != nil {
			return nil, err
		}
	} else {
		_, err := s.BodyReader.Seek(0, io.SeekStart)
		if err != nil {
			return nil, err
		}
		req, err = http.NewRequest(m, u.String(), s.BodyReader)
		if err != nil {
			return nil, err
		}
	}
	if s.Header == nil {
		req.Header = http.Header{}
	} else {
		req.Header = s.Header
	}
	if s.BasicAuthUser != "" || s.BasicAuthPassword != "" {
		req.SetBasicAuth(s.BasicAuthUser, s.BasicAuthPassword)
	}
	return req, nil
}

type RetryableRequest struct {
	MaxAttempts float64
	MaxDuration time.Duration
	RetryDelay  time.Duration
}
type Requests struct {
	Logger    hclog.Logger
	Client    *http.Client
	Retryable RetryableRequest
}

func New(logger hclog.Logger, client *http.Client) Requests {
	req := Requests{
		Logger: logger,
		Client: client,
	}
	return req
}

func NewRetryableDefault(logger hclog.Logger, client *http.Client) Requests {
	req := Requests{
		Logger: logger,
		Client: client,
	}
	req.Retryable.MaxAttempts = 10
	req.Retryable.MaxDuration = 500 * time.Millisecond
	req.Retryable.RetryDelay = 5 * time.Minute
	return req
}

func (opts Requests) retryDo(ctx context.Context, req Request) (resp *http.Response, rerr error) {
	started := time.Now()

	retry := opts.Retryable
	retries := retry.MaxAttempts
	count := 0

	for time.Since(started) < retry.MaxDuration {
		req2, err := req.Request()
		if err != nil {
			rerr = err
			return
		}
		resp, err = opts.Client.Do(req2)
		if err != nil {
			rerr = err
			return
		}

		// if this request looks like a normal, non-retryable response
		// then just return it without attempting a retry
		if (resp.StatusCode >= 200 && resp.StatusCode < 300) ||
			resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusPaymentRequired ||
			resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound ||
			resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusPermanentRedirect ||
			resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusConflict ||
			resp.StatusCode == http.StatusRequestEntityTooLarge || resp.StatusCode == http.StatusRequestedRangeNotSatisfiable ||
			resp.StatusCode == http.StatusRequestHeaderFieldsTooLarge || resp.StatusCode == http.StatusBadRequest ||
			resp.StatusCode == http.StatusUnprocessableEntity || resp.StatusCode == http.StatusInternalServerError {
			return
		}

		// request failed here, will see if we want to retry

		if resp.Body != nil {
			ioutil.ReadAll(resp.Body)
			resp.Body.Close()
		}

		if retry.RetryDelay > 0 {
			remaining := math.Min(float64(retry.MaxDuration-time.Since(started)), float64(retry.RetryDelay))
			select {
			case <-ctx.Done():
				return nil, context.Canceled
			case <-time.After(time.Duration(remaining)):
			}
		}
		retries--
		if retries <= 0 {
			return
		}
		count++
		opts.Logger.Info("request failed, will retry", "count", count, "url", req2.URL.String())
	}
	return
}

type Result struct {
	Resp         *http.Response
	ErrorContext func(error) error
}

// Do makes an http request. It preserves both request and response body for logging purposes.
// Returns logError function that logs the passed error together with request and response body for easier debugging.
func (opts Requests) Do(ctx context.Context, req Request) (res Result, rerr error) {
	logger := opts.Logger

	req2, err := req.Request()
	if err != nil {
		rerr = err
		return
	}

	var resp *http.Response
	if opts.Retryable.MaxAttempts == 0 {
		req2 = req2.WithContext(ctx)
		resp, err = opts.Client.Do(req2)
	} else {
		resp, err = opts.retryDo(ctx, req)
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		rerr = err
		return
	}
	err = resp.Body.Close()
	if err != nil {
		rerr = err
		return
	}
	resp.Body = ioutil.NopCloser(bytes.NewReader(respBody))
	res.Resp = resp
	res.ErrorContext = func(err error) error {
		reqBody := req.Body
		if req.BodyReader != nil {
			_, err := req.BodyReader.Seek(0, io.SeekStart)
			if err != nil {
				return err
			}
			reqBody, err = ioutil.ReadAll(req.BodyReader)
			if err != nil {
				return err
			}
		}
		url := req2.URL.String()
		logger.Debug("error processing response", "err", err.Error(), "url", url, "response_code", resp.StatusCode, "request_body", string(reqBody), "response_body", string(respBody))
		return fmt.Errorf("request failed url: %v err: %w", url, err)
	}
	return
}

type StatusCodeError struct {
	WantStart int
	WantEnd   int
	Got       int
}

func (s StatusCodeError) Error() string {
	return fmt.Sprintf(`wanted status code %v..%v, got %v`, s.WantStart, s.WantEnd, s.Got)
}

func AssertStatusCode(code int, wantStart, wantEnd int) error {
	if !(code >= wantStart && code <= wantEnd) {
		return StatusCodeError{WantStart: wantStart, WantEnd: wantEnd, Got: code}
	}
	return nil
}

// JSON makes http request and unmarshals resulting json. Returns errors on StatusCode != 200. Logs request and response body on errors.
func (opts Requests) JSON(
	req Request,
	res interface{}) (resp Result, rerr error) {

	if req.Header == nil {
		req.Header = http.Header{}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	var err error
	resp, err = opts.Do(context.TODO(), req)
	if err != nil {
		rerr = err
		return
	}
	resp2 := resp.Resp
	err = AssertStatusCode(resp2.StatusCode, 200, 299)
	if err != nil {
		rerr = resp.ErrorContext(err)
		return
	}
	b, err := ioutil.ReadAll(resp2.Body)
	if err != nil {
		rerr = resp.ErrorContext(err)
		return
	}
	if len(b) != 0 {
		err = json.Unmarshal(b, &res)
		if err != nil {
			rerr = resp.ErrorContext(err)
			return
		}
	}
	return
}
