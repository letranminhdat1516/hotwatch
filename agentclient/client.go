// Package agentclient talks to a hotwatch-agent over any net.Conn dialer
// (direct TCP or an SSH tunnel) and implements source.Source.
package agentclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"hotwatch/model"
	"hotwatch/source"
)

// Response size caps (bytes).
const (
	maxSnapshot   = 1 << 20
	maxProcesses  = 8 << 20
	maxContainers = 2 << 20
	maxInfo       = 64 << 10
	maxErrBody    = 64 << 10
	maxLogLine    = 256 << 10

	defaultTimeout = 3 * time.Second
	baseURL        = "http://agent/v1" // placeholder host; the dialer decides where it goes
)

// Client implements source.Source against a remote agent.
type Client struct {
	hc      *http.Client
	token   string
	timeout time.Duration
}

var _ source.Source = (*Client)(nil)

// New returns a Client that reaches the agent through dial and authenticates
// with token.
func New(dial func(ctx context.Context) (net.Conn, error), token string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
		// Fail fast if the agent accepts the connection but never answers.
		// Streams send headers immediately, so this does not affect logs.
		ResponseHeaderTimeout: defaultTimeout,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       60 * time.Second,
		Proxy:                 nil,
	}
	return &Client{hc: &http.Client{Transport: tr}, token: token, timeout: defaultTimeout}
}

func (c *Client) newReq(ctx context.Context, method, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// do performs a bounded request and returns the body (at most limit bytes).
// A non-2xx status becomes *model.APIError.
func (c *Client) do(ctx context.Context, method, path string, body []byte, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := c.newReq(ctx, method, path)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, decodeAPIError(res)
	}
	if res.ContentLength > limit {
		return nil, tooLarge(path, limit)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, tooLarge(path, limit)
	}
	return b, nil
}

func tooLarge(path string, limit int64) error {
	return fmt.Errorf("agent response for %s too large (limit %d bytes)", path, limit)
}

// decodeAPIError turns an error response into *model.APIError, falling back to
// a code derived from the status when the body is not an APIError.
func decodeAPIError(res *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(res.Body, maxErrBody))
	var ae model.APIError
	if json.Unmarshal(b, &ae) == nil && ae.Code != "" {
		return &ae
	}
	code := "internal"
	switch res.StatusCode {
	case 400:
		code = "bad_request"
	case 401:
		code = "unauthorized"
	case 403:
		code = "forbidden"
	case 404:
		code = "not_found"
	case 409:
		code = "conflict"
	case 429:
		code = "too_many_requests"
	case 502:
		code = "docker_unavailable"
	}
	return &model.APIError{Code: code, Message: "agent returned HTTP " + strconv.Itoa(res.StatusCode)}
}

func getJSON[T any](ctx context.Context, c *Client, path string, limit int64) (T, error) {
	var v T
	b, err := c.do(ctx, http.MethodGet, path, nil, limit)
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return v, fmt.Errorf("decode %s: %w", path, err)
	}
	return v, nil
}

func (c *Client) Info(ctx context.Context) (model.HostInfo, error) {
	return getJSON[model.HostInfo](ctx, c, "/info", maxInfo)
}

func (c *Client) Snapshot(ctx context.Context) (model.Snapshot, error) {
	return getJSON[model.Snapshot](ctx, c, "/snapshot", maxSnapshot)
}

func (c *Client) Processes(ctx context.Context) ([]model.Process, error) {
	return getJSON[[]model.Process](ctx, c, "/processes", maxProcesses)
}

func (c *Client) Containers(ctx context.Context) ([]model.Container, error) {
	return getJSON[[]model.Container](ctx, c, "/containers", maxContainers)
}

func (c *Client) Signal(ctx context.Context, pid int32, start time.Time, sig model.Signal) error {
	body, err := json.Marshal(map[string]string{"signal": string(sig), "startTime": start.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, "/processes/"+strconv.FormatInt(int64(pid), 10)+"/signal", body, maxErrBody)
	return err
}

func (c *Client) ContainerAction(ctx context.Context, id string, act model.ContainerAction) error {
	_, err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/"+url.PathEscape(string(act)), nil, maxErrBody)
	return err
}

// ContainerLogs streams log lines. The channel closes when the stream ends,
// the connection fails or ctx is done. No per-request timeout applies.
func (c *Client) ContainerLogs(ctx context.Context, id string, tail int, follow bool) (<-chan model.LogLine, error) {
	f := "0"
	if follow {
		f = "1"
	}
	path := "/containers/" + url.PathEscape(id) + "/logs?tail=" + strconv.Itoa(tail) + "&follow=" + f
	req, err := c.newReq(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/x-ndjson")
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		defer res.Body.Close()
		return nil, decodeAPIError(res)
	}
	ch := make(chan model.LogLine)
	go func() {
		defer close(ch)
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 0, 64<<10), maxLogLine)
		for sc.Scan() {
			if len(bytes.TrimSpace(sc.Bytes())) == 0 {
				continue
			}
			var l model.LogLine
			if json.Unmarshal(sc.Bytes(), &l) != nil {
				return
			}
			select {
			case ch <- l:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
