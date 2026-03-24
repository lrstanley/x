// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpccache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config is the configuration for the cache transport.
type Config struct {
	// BaseTransport is the wrapped upstream transport. Defaults to
	// [net/http.DefaultTransport].
	BaseTransport http.RoundTripper
	// Storage is the cache storage backend. Defaults to [NewMemoryStorage] with
	// 1024 max entries and a 7-day max age.
	Storage Storage

	// Logger is used for cache decision. Defaults to [slog.DiscardHandler].
	Logger *slog.Logger
	// LogLevel is the [slog.Level] used by the built-in decision log. Defaults
	// to [slog.LevelDebug].
	LogLevel *slog.Level

	// LogDecisionFunc is called whenever the cache makes a hit/miss/bypass
	// decision. Defaults to a structured log call at LogLevel.
	LogDecisionFunc func(ctx context.Context, logger *slog.Logger, req *http.Request, decision CacheStatus, reason CacheReason, attrs ...slog.Attr)

	// ShouldCacheRequest decides if a request is eligible for caching. Defaults
	// to [DefaultShouldCacheRequest]. If you override this, it's still recommended
	// to call [DefaultShouldCacheRequest] after your custom logic:
	//
	//	cfg.ShouldCacheRequest = func(req *http.Request) bool {
	//	    if req.URL.Host == "internal.api" {
	//	        return false
	//	    }
	//	    return httpccache.DefaultShouldCacheRequest(req)
	//	}
	ShouldCacheRequest func(*http.Request) bool
	// CacheKeyFunc generates the storage key for cache lookups and writes.
	CacheKeyFunc func(r *http.Request, ignoredHeaders []string) string
	// IgnoredCacheKeyHeaders are headers that should be ignored by the default
	// cache key function because they are typically per-request metadata
	// (request ID, tracing IDs, etc). Defaults to [DefaultIgnoredCacheKeyHeaders].
	IgnoredCacheKeyHeaders []string
	// AllowAuthorizationCaching, when true, relaxes conservative authorization
	// caching behavior. Defaults to false.
	AllowAuthorizationCaching bool
	// AllowHeuristicFreshness, when true, enables Last-Modified heuristic
	// freshness when explicit freshness directives are missing. Defaults to
	// false.
	AllowHeuristicFreshness bool
	// MaxObjectSize is the maximum response body size (bytes) that will be
	// stored. Zero means unlimited. Defaults to 0 (unlimited).
	MaxObjectSize int64

	// DisableResponseAnnotation, when true, disables adding cache metadata
	// headers (Via, CacheStatusHeader, CacheReasonHeader) to responses.
	DisableResponseAnnotation bool
	// ViaProduct is the token added to the Via header when annotation is
	// enabled. Defaults to "httpccache".
	ViaProduct string
	// CacheStatusHeader is the response header name used for cache status
	// metadata. Defaults to [DefaultCacheStatusHeader] ("X-Cache-Status").
	CacheStatusHeader string
	// CacheReasonHeader is the response header name used for cache reason
	// metadata. Defaults to [DefaultCacheReasonHeader] ("X-Cache-Reason").
	CacheReasonHeader string
}

// Validate validates and sets defaults for the config.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config cannot be nil")
	}

	if c.BaseTransport == nil {
		c.BaseTransport = http.DefaultTransport
	}
	if c.Storage == nil {
		c.Storage = NewMemoryStorage(1024, 7*24*time.Hour)
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.DiscardHandler)
	}
	if c.LogLevel == nil {
		c.LogLevel = new(slog.LevelDebug)
	}
	if c.ShouldCacheRequest == nil {
		c.ShouldCacheRequest = DefaultShouldCacheRequest
	}
	if c.IgnoredCacheKeyHeaders == nil {
		c.IgnoredCacheKeyHeaders = DefaultIgnoredCacheKeyHeaders
	}
	if c.CacheKeyFunc == nil {
		c.CacheKeyFunc = CanonicalCacheKey
	}
	if c.ViaProduct == "" {
		c.ViaProduct = "httpccache"
	}
	if c.CacheStatusHeader == "" {
		c.CacheStatusHeader = DefaultCacheStatusHeader
	}
	if c.CacheReasonHeader == "" {
		c.CacheReasonHeader = DefaultCacheReasonHeader
	}
	if c.LogDecisionFunc == nil {
		c.LogDecisionFunc = func(ctx context.Context, logger *slog.Logger, req *http.Request, decision CacheStatus, reason CacheReason, attrs ...slog.Attr) {
			logger.LogAttrs(
				ctx,
				*c.LogLevel,
				"http cache decision",
				append(
					attrs,
					slog.String("decision", string(decision)),
					slog.String("reason", string(reason)),
					slog.String("method", req.Method),
					slog.String("url", req.URL.String()),
				)...,
			)
		}
	}

	return nil
}

type transport struct {
	config *Config
}

// NewTransport creates a new cache round tripper.
func NewTransport(config *Config) http.RoundTripper {
	if config == nil {
		config = &Config{}
	}
	if err := config.Validate(); err != nil {
		panic(err)
	}
	return &transport{config: config}
}

// NewClient creates a new [http.Client] using the cache transport.
func NewClient(config *Config) *http.Client {
	return &http.Client{
		Timeout:   60 * time.Second,
		Transport: NewTransport(config),
	}
}

// RoundTrip implements [http.RoundTripper], attempting cached retrieval first,
// then falling back to upstream and storing when policy allows.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	if !t.config.ShouldCacheRequest(req) {
		t.logDecision(ctx, req, StatusBypass, ReasonRequestNotCacheable)
		return t.roundTripUpstream(req, StatusBypass, ReasonRequestNotCacheable)
	}

	key := t.config.CacheKeyFunc(req, t.config.IgnoredCacheKeyHeaders)
	entry, body, err := t.config.Storage.Get(ctx, key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		t.logDecision(ctx, req, StatusBypass, ReasonStorageError, slog.String("error", err.Error()))
		return t.roundTripUpstream(req, StatusBypass, ReasonStorageError)
	}

	if err == nil {
		resp, cacheErr := t.serveCached(req, key, entry, body)
		if resp != nil || cacheErr != nil {
			return resp, cacheErr
		}
	}

	rd := parseRequestDirectives(req)
	if rd.onlyIfCached {
		resp := &http.Response{
			StatusCode: http.StatusGatewayTimeout,
			Status:     fmt.Sprintf("%d %s", http.StatusGatewayTimeout, http.StatusText(http.StatusGatewayTimeout)),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}
		t.annotateResponse(resp, StatusBypass, ReasonOnlyIfCachedUnsatisfied)
		t.logDecision(ctx, req, StatusBypass, ReasonOnlyIfCachedUnsatisfied)
		return resp, nil
	}

	resp, err := t.roundTripUpstream(req, StatusMiss, ReasonCacheMiss)
	if err != nil {
		return nil, err
	}
	return t.handleUpstreamResponse(req, key, resp)
}

// serveCached attempts to serve a response from a cached record. It returns
// (nil, nil) when the cached record cannot satisfy the request and the caller
// should fall through to upstream.
func (t *transport) serveCached(req *http.Request, key string, entry *CacheEntry, bodyRC io.ReadCloser) (*http.Response, error) {
	ctx := req.Context()

	if !varyAllowsRequest(entry, req) {
		if bodyRC != nil {
			_ = bodyRC.Close()
		}
		t.logDecision(ctx, req, StatusBypass, ReasonVaryMismatch)
		return nil, nil //nolint:nilnil // nil,nil signals fall-through to upstream.
	}

	if requestFreshEnough(req, entry) {
		resp := entry.toResponse(req, bodyRC)
		t.annotateResponse(resp, StatusHit, ReasonCacheHit)
		t.logDecision(ctx, req, StatusHit, ReasonCacheHit)
		return resp, nil
	}

	if entry.ResponseHeader.Get("ETag") != "" || entry.ResponseHeader.Get("Last-Modified") != "" {
		resp, revalidated, reason, revalErr := t.revalidate(req, key, entry, bodyRC)
		if revalErr != nil {
			t.logDecision(ctx, req, StatusBypass, ReasonUpstreamRevalidationFailed, slog.String("error", revalErr.Error()))
			return nil, revalErr
		}
		if revalidated {
			t.annotateResponse(resp, StatusRevalidated, reason)
			t.logDecision(ctx, req, StatusRevalidated, reason)
			return resp, nil
		}
		return t.handleUpstreamResponse(req, key, resp)
	}

	if bodyRC != nil {
		_ = bodyRC.Close()
	}
	t.logDecision(ctx, req, StatusMiss, ReasonStaleRequiresRevalidation)
	return nil, nil //nolint:nilnil // nil,nil signals fall-through to upstream.
}

// revalidate sends a conditional upstream request for a stale cached object.
func (t *transport) revalidate(req *http.Request, key string, entry *CacheEntry, bodyRC io.ReadCloser) (*http.Response, bool, CacheReason, error) {
	now := time.Now()
	revalidateReq := req.Clone(req.Context())
	if etag := entry.ResponseHeader.Get("ETag"); etag != "" {
		revalidateReq.Header.Set("If-None-Match", etag)
	}
	if lm := entry.ResponseHeader.Get("Last-Modified"); lm != "" {
		revalidateReq.Header.Set("If-Modified-Since", lm)
	}

	resp, err := t.roundTripUpstream(revalidateReq, StatusMiss, ReasonStaleRequiresRevalidation)
	if err != nil {
		if bodyRC != nil {
			_ = bodyRC.Close()
		}
		return nil, false, ReasonUpstreamRevalidationFailed, err
	}

	if resp.StatusCode != http.StatusNotModified {
		if bodyRC != nil {
			_ = bodyRC.Close()
		}
		return resp, false, ReasonCacheMiss, nil
	}
	_ = resp.Body.Close()

	cachedResp := entry.toResponse(req, bodyRC)
	for key, vals := range resp.Header {
		cachedResp.Header[key] = append([]string(nil), vals...)
	}

	policy := evaluateResponsePolicy(req, cachedResp, t.config.AllowAuthorizationCaching, t.config.AllowHeuristicFreshness)
	if !policy.cacheable {
		_ = t.config.Storage.Delete(req.Context(), key)
		return cachedResp, true, policy.reason, nil
	}

	entry.ResponseHeader = cachedResp.Header.Clone()
	entry.ExpiresAt = policy.expiresAt
	entry.StoredAt = now
	entry.CreatedAt = now
	_ = t.config.Storage.Set(req.Context(), key, entry, nil)

	return cachedResp, true, ReasonCacheHit, nil
}

// handleUpstreamResponse decides whether an upstream response should be stored.
func (t *transport) handleUpstreamResponse(req *http.Request, key string, resp *http.Response) (*http.Response, error) {
	ctx := req.Context()
	now := time.Now()

	policy := evaluateResponsePolicy(req, resp, t.config.AllowAuthorizationCaching, t.config.AllowHeuristicFreshness)
	if !policy.cacheable {
		t.logDecision(ctx, req, StatusMiss, policy.reason)
		t.annotateResponse(resp, StatusMiss, policy.reason)
		return resp, nil
	}

	vary := parseVary(resp)
	if slices.Contains(vary, "*") {
		t.logDecision(ctx, req, StatusMiss, ReasonVaryWildcard)
		t.annotateResponse(resp, StatusMiss, ReasonVaryWildcard)
		return resp, nil
	}

	varyValues := make(map[string]string, len(vary))
	for _, vk := range vary {
		varyValues[vk] = strings.Join(req.Header.Values(vk), ",")
	}

	entry := &CacheEntry{
		Key:            key,
		Method:         req.Method,
		URL:            req.URL.String(),
		StoredAt:       now,
		ExpiresAt:      policy.expiresAt,
		CreatedAt:      now,
		ResponseStatus: resp.StatusCode,
		ResponseHeader: resp.Header.Clone(),
		VaryValues:     varyValues,
	}

	var body io.Reader = resp.Body
	if t.config.MaxObjectSize > 0 {
		body = NewBodyLimitReader(resp.Body, t.config.MaxObjectSize)
	}

	err := t.config.Storage.Set(ctx, key, entry, body)
	if err != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(strings.NewReader(""))
		resp.ContentLength = 0
		if errors.Is(err, ErrReadLimitExceeded) {
			_ = t.config.Storage.Delete(ctx, key)
			t.logDecision(ctx, req, StatusMiss, ReasonObjectTooLarge, slog.Int64("max", t.config.MaxObjectSize))
			t.annotateResponse(resp, StatusMiss, ReasonObjectTooLarge)
			return resp, nil
		}
		t.logDecision(ctx, req, StatusMiss, ReasonStorageError, slog.String("error", err.Error()))
		t.annotateResponse(resp, StatusMiss, ReasonStorageError)
		return resp, nil
	}

	storedMeta, bodyRC, err := t.config.Storage.Get(ctx, key)
	if err != nil {
		resp.Body = io.NopCloser(strings.NewReader(""))
		resp.ContentLength = 0
		t.logDecision(ctx, req, StatusMiss, ReasonStorageError, slog.String("error", err.Error()))
		t.annotateResponse(resp, StatusMiss, ReasonStorageError)
		return resp, nil
	}
	resp.Body = bodyRC
	resp.ContentLength = storedMeta.BodySize
	resp.Header = storedMeta.ResponseHeader.Clone()
	resp.StatusCode = storedMeta.ResponseStatus
	resp.Status = strconv.Itoa(storedMeta.ResponseStatus) + " " + http.StatusText(storedMeta.ResponseStatus)

	t.logDecision(ctx, req, StatusMiss, ReasonStored)
	t.annotateResponse(resp, StatusMiss, ReasonStored)
	return resp, nil
}

// roundTripUpstream delegates to the configured base transport.
func (t *transport) roundTripUpstream(req *http.Request, status CacheStatus, reason CacheReason) (*http.Response, error) {
	resp, err := t.config.BaseTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	t.annotateResponse(resp, status, reason)
	return resp, nil
}

// DefaultIgnoredCacheKeyHeaders are per-request metadata headers ignored by
// the default canonical cache key to avoid unnecessary cache fragmentation.
var DefaultIgnoredCacheKeyHeaders = []string{
	"X-Request-ID",
	"Request-ID",
	"X-Correlation-ID",
	"Correlation-ID",
	"X-Trade-ID",
	"Trade-ID",
	"Traceparent",
	"Tracestate",
	"B3",
	"X-B3-TraceID",
	"X-B3-SpanID",
	"X-B3-ParentSpanID",
	"X-B3-Sampled",
	"X-B3-Flags",
	"X-Amzn-Trace-Id",
}

// CanonicalCacheKey builds a stable cache key from a request's method, URL,
// URL userinfo, and headers while excluding ignored header keys.
func CanonicalCacheKey(req *http.Request, ignoredHeaders []string) string {
	u := *req.URL
	u.Fragment = ""
	u.User = nil
	key := req.Method + " " + u.String()
	hasData := false
	hasher := sha256.New()
	writePart := func(s string) {
		_, _ = hasher.Write([]byte(s))
		_, _ = hasher.Write([]byte{0})
	}

	if req.URL.User != nil {
		hasData = true
		username := req.URL.User.Username()
		password, _ := req.URL.User.Password()
		writePart("url-user")
		writePart(username)
		writePart("url-pass")
		writePart(password)
	}

	ignored := make(map[string]struct{}, len(ignoredHeaders))
	for _, header := range ignoredHeaders {
		if header == "" {
			continue
		}
		ignored[http.CanonicalHeaderKey(header)] = struct{}{}
	}

	keys := make([]string, 0, len(req.Header))
	for key := range req.Header {
		ck := http.CanonicalHeaderKey(key)
		if _, skip := ignored[ck]; skip {
			continue
		}
		keys = append(keys, ck)
	}
	sort.Strings(keys)

	for _, header := range keys {
		values := append([]string(nil), req.Header.Values(header)...)
		sort.Strings(values)
		hasData = true
		writePart("header")
		writePart(header)
		for _, value := range values {
			writePart(value)
		}
	}

	if hasData {
		key += " req:" + hex.EncodeToString(hasher.Sum(nil))
	}
	return key
}

// DefaultShouldCacheRequest is the built-in request eligibility check used
// when [Config.ShouldCacheRequest] is nil. It returns true for GET and HEAD
// requests that do not carry conditional headers ([If-None-Match],
// [If-Modified-Since], etc.), [Range] headers, or a [Cache-Control]: no-store
// directive. Callers can wrap this function with additional logic and pass the
// wrapper via [Config.ShouldCacheRequest]:
//
//	cfg.ShouldCacheRequest = func(req *http.Request) bool {
//	    if req.URL.Host == "internal.api" {
//	        return false
//	    }
//	    return httpccache.DefaultShouldCacheRequest(req)
//	}
//
// [If-None-Match]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/If-None-Match
// [If-Modified-Since]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/If-Modified-Since
// [Range]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Range
// [Cache-Control]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Cache-Control
func DefaultShouldCacheRequest(req *http.Request) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}
	if requestHasConditionals(req) {
		return false
	}
	if requestHasRange(req) {
		return false
	}
	_, noStore := parseCacheControl(req.Header.Values("Cache-Control"))["no-store"]
	return !noStore
}

// annotateResponse marks the response with cache metadata headers.
func (t *transport) annotateResponse(resp *http.Response, status CacheStatus, reason CacheReason) {
	if resp == nil || t.config.DisableResponseAnnotation {
		return
	}
	if resp.Header == nil {
		resp.Header = make(http.Header)
	}
	resp.Header.Add("Via", "1.1 "+t.config.ViaProduct)
	resp.Header.Set(t.config.CacheStatusHeader, string(status))
	resp.Header.Set(t.config.CacheReasonHeader, string(reason))
}

// logDecision emits the cache decision to the configured logger hook.
func (t *transport) logDecision(ctx context.Context, req *http.Request, decision CacheStatus, reason CacheReason, attrs ...slog.Attr) {
	t.config.LogDecisionFunc(ctx, t.config.Logger, req, decision, reason, attrs...)
}

// CacheStatusFromResponse returns the typed cache status from response headers.
func CacheStatusFromResponse(resp *http.Response) CacheStatus {
	if resp == nil || resp.Header == nil {
		return ""
	}
	return CacheStatus(resp.Header.Get(DefaultCacheStatusHeader))
}

// CacheReasonFromResponse returns the typed cache reason from response headers.
func CacheReasonFromResponse(resp *http.Response) CacheReason {
	if resp == nil || resp.Header == nil {
		return ""
	}
	return CacheReason(resp.Header.Get(DefaultCacheReasonHeader))
}

// IsCacheHit reports if the response was served from cache.
func IsCacheHit(resp *http.Response) bool {
	return CacheStatusFromResponse(resp) == StatusHit
}

// IsCacheMiss reports if the response came from upstream.
func IsCacheMiss(resp *http.Response) bool {
	return CacheStatusFromResponse(resp) == StatusMiss
}

// IsCacheRevalidated reports if cache revalidation succeeded via 304.
func IsCacheRevalidated(resp *http.Response) bool {
	return CacheStatusFromResponse(resp) == StatusRevalidated
}

// IsFromCache reports if the response body came from cache material.
func IsFromCache(resp *http.Response) bool {
	return IsCacheHit(resp) || IsCacheRevalidated(resp)
}
