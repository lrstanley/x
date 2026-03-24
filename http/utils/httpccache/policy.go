// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpccache

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// requestDirectives represents the parsed Cache-Control directives from a request.
type requestDirectives struct {
	// noStore is true when the request contains Cache-Control: no-store.
	noStore bool
	// noCache is true when the request contains Cache-Control: no-cache,
	// forcing revalidation even if a fresh entry exists.
	noCache bool
	// onlyIfCached is true when the request contains Cache-Control:
	// only-if-cached, requiring a cached response or 504.
	onlyIfCached bool
	// maxAge, when non-nil, is the client's maximum acceptable age for a
	// cached response (Cache-Control: max-age).
	maxAge *time.Duration
	// minFresh, when non-nil, is the minimum remaining freshness lifetime
	// the client requires (Cache-Control: min-fresh).
	minFresh *time.Duration
}

// responsePolicy represents the caching policy evaluation for a response.
type responsePolicy struct {
	// cacheable indicates whether the response may be stored.
	cacheable bool
	// reason is the machine-readable explanation for the caching decision.
	reason CacheReason
	// expiresAt is the computed point in time when the cached entry becomes
	// stale. Only meaningful when cacheable is true.
	expiresAt time.Time
}

// parseCacheControl parses [Cache-Control] directives while preferring the first
// occurrence for duplicated directive keys.
//
// [Cache-Control]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Cache-Control
func parseCacheControl(values []string) map[string]string {
	out := make(map[string]string)
	for _, raw := range values {
		for part := range strings.SplitSeq(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			key := part
			val := ""
			if before, after, ok := strings.Cut(part, "="); ok {
				key = strings.TrimSpace(before)
				val = strings.Trim(strings.TrimSpace(after), "\"")
			}
			key = strings.ToLower(key)
			if _, exists := out[key]; exists {
				continue // use first value when duplicates are present.
			}
			out[key] = val
		}
	}
	return out
}

// parseRequestDirectives parses request directives used by cache selection.
func parseRequestDirectives(req *http.Request) requestDirectives {
	cc := parseCacheControl(req.Header.Values("Cache-Control"))
	rd := requestDirectives{}
	_, rd.noStore = cc["no-store"]
	_, rd.noCache = cc["no-cache"]
	_, rd.onlyIfCached = cc["only-if-cached"]
	if d, ok := parseSecondsDirective(cc, "max-age"); ok {
		rd.maxAge = &d
	}
	if d, ok := parseSecondsDirective(cc, "min-fresh"); ok {
		rd.minFresh = &d
	}
	return rd
}

// evaluateResponsePolicy decides whether a response is cacheable and computes
// an expiry timestamp when it is.
func evaluateResponsePolicy(req *http.Request, resp *http.Response, allowAuthCache, allowHeuristic bool) responsePolicy {
	now := time.Now()
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return responsePolicy{reason: ReasonMethodNotCacheable}
	}

	reqCC := parseCacheControl(req.Header.Values("Cache-Control"))
	if _, ok := reqCC["no-store"]; ok {
		return responsePolicy{reason: ReasonRequestNoStore}
	}

	respCC := parseCacheControl(resp.Header.Values("Cache-Control"))
	if _, ok := respCC["no-store"]; ok {
		return responsePolicy{reason: ReasonResponseNoStore}
	}

	if req.Header.Get("Authorization") != "" && !allowAuthCache {
		_, pub := respCC["public"]
		_, mr := respCC["must-revalidate"]
		_, sma := respCC["s-maxage"]
		if !pub && !mr && !sma {
			return responsePolicy{reason: ReasonAuthorizationNotAllowed}
		}
	}

	if _, pub := respCC["public"]; !isStatusCacheable(resp.StatusCode) && !pub {
		return responsePolicy{reason: ReasonStatusNotCacheable}
	}

	if _, ok := respCC["no-cache"]; ok {
		// Stored but immediately stale, forcing validation before reuse.
		return responsePolicy{cacheable: true, reason: ReasonStored, expiresAt: now}
	}

	if maxAge, ok := parseSecondsDirective(respCC, "max-age"); ok {
		return responsePolicy{cacheable: true, reason: ReasonStored, expiresAt: now.Add(maxAge)}
	}

	expires := resp.Header.Get("Expires")
	if expires != "" {
		date := responseDate(resp, now)
		if expAt, err := http.ParseTime(expires); err == nil {
			if expAt.After(date) {
				return responsePolicy{cacheable: true, reason: ReasonStored, expiresAt: expAt}
			}
			// Explicitly stale.
			return responsePolicy{cacheable: true, reason: ReasonStored, expiresAt: now}
		}
		return responsePolicy{reason: ReasonInvalidFreshness}
	}

	if allowHeuristic {
		if lm, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil && lm.Before(now) {
			ttl := now.Sub(lm) / 10
			if ttl > 0 {
				return responsePolicy{cacheable: true, reason: ReasonStored, expiresAt: now.Add(ttl)}
			}
		}
	}

	return responsePolicy{reason: ReasonNoExplicitFreshness}
}

// responseDate returns the parsed [Date] header or fallback when missing/invalid.
//
// [Date]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Date
func responseDate(resp *http.Response, fallback time.Time) time.Time {
	if v := resp.Header.Get("Date"); v != "" {
		if parsed, err := http.ParseTime(v); err == nil {
			return parsed
		}
	}
	return fallback
}

// parseSecondsDirective parses delta-second directive values.
func parseSecondsDirective(directives map[string]string, key string) (time.Duration, bool) {
	v, ok := directives[key]
	if !ok {
		return 0, false
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs < 0 {
		return 0, false
	}
	return time.Duration(secs) * time.Second, true
}

// isStatusCacheable returns true for status codes considered cacheable by
// default for this client-side implementation.
func isStatusCacheable(code int) bool {
	switch code {
	case http.StatusOK,
		http.StatusNonAuthoritativeInfo,
		http.StatusNoContent,
		http.StatusPartialContent,
		http.StatusMultipleChoices,
		http.StatusMovedPermanently,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusGone,
		http.StatusRequestURITooLong,
		http.StatusNotImplemented:
		return true
	default:
		return false
	}
}

// requestHasConditionals reports whether any If-* condition header is present.
func requestHasConditionals(req *http.Request) bool {
	for key := range req.Header {
		if strings.HasPrefix(http.CanonicalHeaderKey(key), "If-") {
			return true
		}
	}
	return false
}

// requestHasRange reports whether the request has a [Range] header.
//
// [Range]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Range
func requestHasRange(req *http.Request) bool {
	return req.Header.Get("Range") != ""
}

// parseVary parses [Vary] header field names into canonicalized header names.
//
// [Vary]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Vary
func parseVary(resp *http.Response) []string {
	var out []string
	for _, raw := range resp.Header.Values("Vary") {
		for part := range strings.SplitSeq(raw, ",") {
			key := http.CanonicalHeaderKey(strings.TrimSpace(part))
			if key == "" {
				continue
			}
			out = append(out, key)
		}
	}
	return out
}

// varyAllowsRequest checks whether the request matches the stored [Vary] values.
//
// [Vary]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Vary
func varyAllowsRequest(entry *CacheEntry, req *http.Request) bool {
	vary := parseVary(&http.Response{Header: entry.ResponseHeader})
	if len(vary) == 0 {
		return true
	}
	for _, header := range vary {
		if header == "*" {
			return false
		}
		if entry.VaryValues[header] != strings.Join(req.Header.Values(header), ",") {
			return false
		}
	}
	return true
}

// requestFreshEnough checks entry freshness against request freshness controls.
func requestFreshEnough(req *http.Request, entry *CacheEntry) bool {
	now := time.Now()
	if entry.ExpiresAt.IsZero() || !now.Before(entry.ExpiresAt) {
		return false
	}
	directives := parseRequestDirectives(req)
	if directives.noCache {
		return false
	}
	age := now.Sub(entry.StoredAt)
	if directives.maxAge != nil && age > *directives.maxAge {
		return false
	}

	if directives.minFresh != nil {
		remaining := entry.ExpiresAt.Sub(now)
		if remaining < *directives.minFresh {
			return false
		}
	}

	return true
}
