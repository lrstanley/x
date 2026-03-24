// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpccache

// CacheStatus is the machine-readable cache status value stored in response
// headers and decision logs.
type CacheStatus string

// IsValid reports whether the status is one of the known [CacheStatus] values.
func (s CacheStatus) IsValid() bool {
	switch s {
	case StatusHit, StatusMiss, StatusRevalidated, StatusBypass:
		return true
	default:
		return false
	}
}

const (
	// StatusHit indicates the returned response came directly from cache.
	StatusHit CacheStatus = "hit"
	// StatusMiss indicates an upstream request was required.
	StatusMiss CacheStatus = "miss"
	// StatusRevalidated indicates cache revalidation produced a 304 and the cached
	// object was returned.
	StatusRevalidated CacheStatus = "revalidated"
	// StatusBypass indicates cache logic was skipped for this request.
	StatusBypass CacheStatus = "bypass"
)

const (
	// DefaultCacheStatusHeader is the default response header for cache status.
	DefaultCacheStatusHeader = "X-Cache-Status"
	// DefaultCacheReasonHeader is the default response header for cache reason.
	DefaultCacheReasonHeader = "X-Cache-Reason"
)

// CacheReason is the machine-readable reason attached to cache decisions.
type CacheReason string

// IsValid reports whether the reason is one of the known [CacheReason] values.
func (r CacheReason) IsValid() bool {
	switch r {
	case ReasonMethodNotCacheable,
		ReasonRequestNotCacheable,
		ReasonRequestNoStore,
		ReasonResponseNoStore,
		ReasonStatusNotCacheable,
		ReasonNoExplicitFreshness,
		ReasonInvalidFreshness,
		ReasonVaryMismatch,
		ReasonVaryWildcard,
		ReasonStaleRequiresRevalidation,
		ReasonAuthorizationNotAllowed,
		ReasonConditionalPassthrough,
		ReasonRangePassthrough,
		ReasonStorageError,
		ReasonObjectTooLarge,
		ReasonStored,
		ReasonCacheHit,
		ReasonCacheMiss,
		ReasonOnlyIfCachedUnsatisfied,
		ReasonDecodeEntryFailed,
		ReasonUpstreamRevalidationFailed:
		return true
	default:
		return false
	}
}

const (
	ReasonMethodNotCacheable         CacheReason = "method_not_cacheable"
	ReasonRequestNotCacheable        CacheReason = "request_not_cacheable"
	ReasonRequestNoStore             CacheReason = "request_no_store"
	ReasonResponseNoStore            CacheReason = "response_no_store"
	ReasonStatusNotCacheable         CacheReason = "status_not_cacheable"
	ReasonNoExplicitFreshness        CacheReason = "no_explicit_freshness"
	ReasonInvalidFreshness           CacheReason = "invalid_freshness"
	ReasonVaryMismatch               CacheReason = "vary_mismatch"
	ReasonVaryWildcard               CacheReason = "vary_wildcard"
	ReasonStaleRequiresRevalidation  CacheReason = "stale_requires_revalidation"
	ReasonAuthorizationNotAllowed    CacheReason = "authorization_not_allowed"
	ReasonConditionalPassthrough     CacheReason = "conditional_request_passthrough"
	ReasonRangePassthrough           CacheReason = "range_request_passthrough"
	ReasonStorageError               CacheReason = "storage_error"
	ReasonObjectTooLarge             CacheReason = "object_too_large"
	ReasonStored                     CacheReason = "stored"
	ReasonCacheHit                   CacheReason = "cache_hit"
	ReasonCacheMiss                  CacheReason = "cache_miss"
	ReasonOnlyIfCachedUnsatisfied    CacheReason = "only_if_cached_unsatisfied"
	ReasonDecodeEntryFailed          CacheReason = "decode_entry_failed"
	ReasonUpstreamRevalidationFailed CacheReason = "upstream_revalidation_failed"
)
