package httpclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/cenkalti/backoff"
)

const (
	httpTimeout = time.Second * 30
)

type HTTPOptions struct {
	Endpoint string
	Body     io.Reader
	Headers  http.Header
	Params   url.Values

	// Ref: https://github.com/cenkalti/backoff/blob/a78d3804c2c84f0a3178648138442c9b07665bda/exponential.go#L76
	// DefaultInitialInterval     = 500 * time.Millisecond
	// DefaultRandomizationFactor = 0.5
	// DefaultMultiplier          = 1.5
	// DefaultMaxInterval         = 60 * time.Second
	// DefaultMaxElapsedTime      = 15 * time.Minute
	ExponentialBackoff *backoff.ExponentialBackOff
}

type HttpClient struct {
	backoffParams *backoff.ExponentialBackOff

	targetUrl string
	body      io.Reader
	headers   http.Header
	params    url.Values
}

func New(
	serviceUrl string,
	options HTTPOptions,
) (*HttpClient, error) {

	if options.Endpoint != "" {
		combo, err := url.ParseRequestURI(serviceUrl)
		if err != nil {
			return nil, err
		}
		combo.Path = path.Join(combo.Path, options.Endpoint)
		serviceUrl = combo.String()
	}

	if options.Headers == nil {
		options.Headers = http.Header{}
	}

	if options.Params == nil {
		options.Params = url.Values{}
	}

	return &HttpClient{
		backoffParams: options.ExponentialBackoff,
		targetUrl:     serviceUrl,
		body:          options.Body,
		headers:       options.Headers,
		params:        options.Params,
	}, nil
}

func (h *HttpClient) Post(ctx context.Context) (*http.Response, error) {
	return h.execute(http.MethodPost, ctx)
}

func (h *HttpClient) Patch(ctx context.Context) (*http.Response, error) {
	return h.execute(http.MethodPatch, ctx)
}

func (h *HttpClient) Get(ctx context.Context) (*http.Response, error) {
	return h.execute(http.MethodGet, ctx)
}

func (h *HttpClient) execute(method string, ctx context.Context) (*http.Response, error) {
	// If there is no backoff, then only execute request once
	if h.backoffParams == nil {
		return h.request(method, ctx)
	}

	// Keep looping through our ticker, waiting for it to tell us when to retry
	var lastErr error
	ticker := backoff.NewTicker(h.backoffParams)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled before successful http response")
		case _, ok := <-ticker.C:
			if !ok {
				return nil, fmt.Errorf("failed to get successful http response to %s after %s. The most recent error was: %s", h.targetUrl, h.backoffParams.MaxElapsedTime.Round(time.Second), lastErr)
			}

			if response, err := h.request(method, ctx); err != nil {
				lastErr = err
			} else {
				return response, err
			}
		}
	}
}

func (h *HttpClient) request(method string, ctx context.Context) (*http.Response, error) {
	// Make our Client
	client := http.Client{
		Timeout: httpTimeout,
	}

	// Build our Request
	request, err := http.NewRequestWithContext(ctx, method, h.targetUrl, h.body)
	if err != nil {
		return nil, fmt.Errorf("failed to build http request: %w", err)
	}

	request.Header = http.Header(h.headers)

	// Add params to request URL
	request.URL.RawQuery = h.params.Encode()

	// Make our Request
	response, err := client.Do(request)
	if err != nil {
		return response, fmt.Errorf("%s request to %s failed: %w", method, h.targetUrl, err)
	}

	// Check if request was successful
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, fmt.Errorf("%s request failed to %s with status %s", method, h.targetUrl, response.Status)
	}

	return response, err
}
