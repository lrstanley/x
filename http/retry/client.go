// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package retry

import (
	"bytes"
	"context"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
)

// DefaultPolicy is the default retry policy. It retries on network errors, 5xx status
// codes, and 429 Too Many Requests. It does not retry on [context.Canceled] or
// [context.DeadlineExceeded], as this is often intentional cancellation from the
// parent caller.
func DefaultPolicy(ctx context.Context, resp *http.Response, err error) bool {
	// Don't retry on [context.Canceled] or [context.DeadlineExceeded].
	if ctx.Err() != nil {
		return false
	}

	if err != nil {
		return true
	}

	if resp.StatusCode == 0 || resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 500 && resp.StatusCode != http.StatusNotImplemented) {
		return true
	}

	return false
}

// DefaultBackoff is the default backoff function. It uses exponential backoff with a
// minimum and maximum duration. It also attempts to parse the [Retry-After] header from
// the response and uses that as the backoff duration if it is present and valid. If
// the [Retry-After] header is not present or invalid, it falls back to the exponential
// backoff calculation.
//
// [Retry-After]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Retry-After
func DefaultBackoff(config *Config, attempt int, resp *http.Response) time.Duration {
	if resp != nil && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) {
		if retryAfter, ok := parseRetryAfterHeader(resp.Header["Retry-After"]); ok {
			if retryAfter > config.MaxRateLimitDuration {
				retryAfter = config.MaxRateLimitDuration
			}
			return retryAfter
		}
	}

	mult := math.Pow(2, float64(attempt)) * float64(config.MinBackoff)
	sleep := time.Duration(mult)

	if float64(sleep) != mult || sleep > config.MaxBackoff {
		sleep = config.MaxBackoff
	}
	return sleep
}

func parseRetryAfterHeader(headers []string) (time.Duration, bool) {
	if len(headers) == 0 {
		return 0, false
	}

	retryAfter := headers[0]
	if retryAfter == "" {
		return 0, false
	}

	// "Retry-After: <int>"
	if sleep, err := strconv.Atoi(retryAfter); err == nil {
		if sleep < 0 {
			return 0, false
		}
		return time.Duration(sleep) * time.Second, true
	}

	// "Retry-After: <rfc1123-date>"
	retryTime, err := time.Parse(time.RFC1123, retryAfter)
	if err != nil {
		return 0, false
	}
	if until := time.Until(retryTime); until < 0 {
		return 0, false
	}
	return time.Until(retryTime), true
}

// Config is the configuration for the retryable transport.
type Config struct {
	// BaseTransport is the base transport to use (will be chained). Defaults to
	// [net/http.DefaultTransport], which allows for connection reuse, HTTP proxy
	// support, etc.
	BaseTransport http.RoundTripper

	// MaxRetries is the maximum number of retries to perform. Defaults to 4.
	MaxRetries int

	// MaxRateLimitDuration is the maximum duration to wait when the server returns
	// 429 Too Many Requests. This can sometimes be a very long time, so depending on
	// your usecase/application, you may not want to wait that long. Defaults to
	// [Config.MaxBackoff].
	MaxRateLimitDuration time.Duration

	// MinBackoff is the minimum backoff duration. Defaults to 1 second.
	MinBackoff time.Duration

	// MaxBackoff is the maximum backoff duration. Defaults to 30 seconds.
	MaxBackoff time.Duration

	// Backoff is a function that calculates the backoff duration based on the attempt
	// number and the response. Defaults to [DefaultBackoff], which uses exponential
	// backoff with the provided minimum and maximum duration.
	Backoff func(config *Config, attempt int, resp *http.Response) time.Duration

	// DefaultPolicy is a function that determines whether to retry based on the context,
	// response and error. Defaults to [DefaultPolicy], which retries on network errors,
	// 5xx status codes, and 429 Too Many Requests. [DefaultPolicy] does not retry on
	// [context.Canceled] or [context.DeadlineExceeded] (as this would be intentional
	// cancellation from the parent caller).
	DefaultPolicy func(context.Context, *http.Response, error) bool

	// RetryCallback is a function that is called right before a retry is attempted. The
	// request and response SHOULD NOT BE MODIFIED. This is useful for logging or other
	// side effects.
	RetryCallback func(ctx context.Context, attempts int, backoff time.Duration, req *http.Request, resp *http.Response, err error)
}

func (c *Config) Validate() error {
	if c == nil {
		panic("Config cannot be nil")
	}

	if c.BaseTransport == nil {
		c.BaseTransport = http.DefaultTransport
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = 4
	}
	if c.MinBackoff <= 0 {
		c.MinBackoff = 1 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Second
	}
	if c.MaxBackoff < c.MinBackoff {
		c.MaxBackoff = c.MinBackoff
	}
	if c.MaxRateLimitDuration <= 0 {
		c.MaxRateLimitDuration = c.MaxBackoff
	}
	if c.Backoff == nil {
		c.Backoff = DefaultBackoff
	}
	if c.DefaultPolicy == nil {
		c.DefaultPolicy = DefaultPolicy
	}

	return nil
}

// NewTransport creates a new [net/http.RoundTripper] that retries requests based on
// the provided config.
func NewTransport(config *Config) http.RoundTripper {
	if config == nil {
		config = &Config{}
	}
	err := config.Validate()
	if err != nil {
		panic(err)
	}
	return &RetryableTransport{Config: config}
}

type RetryableTransport struct {
	Config *Config
}

func (t *RetryableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Config == nil {
		panic("RetryableTransport.Config cannot be nil")
	}

	// Clone the request body.
	var bodyBytes []byte
	if req.Body != nil {
		bodyBytes, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	// Send the request.
	resp, err := t.Config.BaseTransport.RoundTrip(req)
	retries := 0

	for t.Config.DefaultPolicy(req.Context(), resp, err) && retries < t.Config.MaxRetries {
		backoff := t.Config.Backoff(t.Config, retries, resp)

		if t.Config.RetryCallback != nil {
			t.Config.RetryCallback(req.Context(), retries, backoff, req, resp, err)
		}

		// Drain the body so we can reuse the connection.
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}

		// Recreate the request body again.
		if req.Body != nil {
			req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		// Wait for the backoff duration.
		time.Sleep(backoff)

		// Send the request again.
		resp, err = t.Config.BaseTransport.RoundTrip(req)
		retries++
	}

	return resp, err
}

// NewClient is identical to [NewTransport], but returns a higher-level [http.Client]
// instead of an underlying [http.RoundTripper] transport.
func NewClient(config *Config) *http.Client {
	if config == nil {
		config = &Config{}
	}
	err := config.Validate()
	if err != nil {
		panic(err)
	}
	return &http.Client{
		Timeout:   max(config.MaxRateLimitDuration, config.MaxBackoff)*time.Duration(config.MaxRetries) + 5*time.Second,
		Transport: NewTransport(config),
	}
}
