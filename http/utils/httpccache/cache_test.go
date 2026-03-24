package httpccache

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// fakeRoundTripper wraps an http.Handler as an http.RoundTripper without
// network I/O, making it safe for use inside a synctest bubble.
type fakeRoundTripper struct {
	handler http.Handler
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func TestTransportMaxObjectSize(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Cache-Control", "max-age=60")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, strings.Repeat("x", 100))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&Config{
		Storage:       NewMemoryStorage(128, time.Hour),
		MaxObjectSize: 50,
	})

	req1, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/big", http.NoBody)
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	b1, _ := io.ReadAll(resp1.Body)
	_ = resp1.Body.Close()
	if CacheReasonFromResponse(resp1) != ReasonObjectTooLarge {
		t.Fatalf("expected object_too_large, got %q", CacheReasonFromResponse(resp1))
	}
	if len(b1) != 0 {
		t.Fatalf("expected empty body after oversize cache failure, got %d bytes", len(b1))
	}

	req2, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/big", http.NoBody)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	_, _ = io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if calls.Load() != 2 {
		t.Fatalf("expected two upstream calls when body not cached, got %d", calls.Load())
	}
}

func TestTransportCacheHitMiss(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Cache-Control", "max-age=60")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&Config{
		Storage: NewMemoryStorage(128, time.Hour),
	})

	req1, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/x", http.NoBody)
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("request 1 failed: %v", err)
	}
	if !IsCacheMiss(resp1) {
		t.Fatalf("expected first request to be miss, got %q", CacheStatusFromResponse(resp1))
	}
	if resp1.Header.Get("Via") == "" {
		t.Fatalf("expected Via header on response")
	}

	req2, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/x", http.NoBody)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	if !IsCacheHit(resp2) {
		t.Fatalf("expected second request to be hit, got %q", CacheStatusFromResponse(resp2))
	}
	if calls.Load() != 1 {
		t.Fatalf("expected exactly one upstream call, got %d", calls.Load())
	}
}

func TestTransportNoStoreAndMethodBypass(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/nostore" {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "max-age=60")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&Config{Storage: NewMemoryStorage(128, time.Hour)})

	for range 2 {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/nostore", http.NoBody)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("nostore request failed: %v", err)
		}
		if CacheReasonFromResponse(resp) != ReasonResponseNoStore {
			t.Fatalf("expected reason %q, got %q", ReasonResponseNoStore, CacheReasonFromResponse(resp))
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("expected no-store path to call upstream twice, got %d", calls.Load())
	}

	reqPost, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/post", strings.NewReader("body"))
	respPost, err := client.Do(reqPost)
	if err != nil {
		t.Fatalf("post request failed: %v", err)
	}
	if CacheStatusFromResponse(respPost) != StatusBypass {
		t.Fatalf("expected bypass for post, got %q", CacheStatusFromResponse(respPost))
	}
	if CacheReasonFromResponse(respPost) != ReasonRequestNotCacheable {
		t.Fatalf("unexpected post reason %q", CacheReasonFromResponse(respPost))
	}
}

func TestTransportConditionalRangeBypass(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "max-age=60")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&Config{Storage: NewMemoryStorage(128, time.Hour)})

	reqConditional, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/c", http.NoBody)
	reqConditional.Header.Set("If-None-Match", `"abc"`)
	respConditional, err := client.Do(reqConditional)
	if err != nil {
		t.Fatalf("conditional request failed: %v", err)
	}
	if CacheReasonFromResponse(respConditional) != ReasonRequestNotCacheable {
		t.Fatalf("unexpected reason for conditional request: %q", CacheReasonFromResponse(respConditional))
	}

	reqRange, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/r", http.NoBody)
	reqRange.Header.Set("Range", "bytes=0-5")
	respRange, err := client.Do(reqRange)
	if err != nil {
		t.Fatalf("range request failed: %v", err)
	}
	if CacheReasonFromResponse(respRange) != ReasonRequestNotCacheable {
		t.Fatalf("unexpected reason for range request: %q", CacheReasonFromResponse(respRange))
	}
}

func TestTransportRevalidation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Cache-Control", "max-age=0")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("payload"))
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.Header().Set("Cache-Control", "max-age=60")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Cache-Control", "max-age=60")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("payload-2"))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&Config{Storage: NewMemoryStorage(128, time.Hour)})

	req1, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/revalidate", http.NoBody)
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("request 1 failed: %v", err)
	}
	_ = resp1.Body.Close()

	req2, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/revalidate", http.NoBody)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	body, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if !IsCacheRevalidated(resp2) {
		t.Fatalf("expected revalidated status, got %q", CacheStatusFromResponse(resp2))
	}
	if string(body) != "payload" {
		t.Fatalf("expected cached payload body, got %q", string(body))
	}

	req3, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/revalidate", http.NoBody)
	resp3, err := client.Do(req3)
	if err != nil {
		t.Fatalf("request 3 failed: %v", err)
	}
	_ = resp3.Body.Close()
	if !IsCacheHit(resp3) {
		t.Fatalf("expected cache hit after revalidation, got %q", CacheStatusFromResponse(resp3))
	}
}

func TestResponseHelpers(t *testing.T) {
	t.Parallel()

	if CacheStatusFromResponse(nil) != "" || CacheReasonFromResponse(nil) != "" {
		t.Fatalf("nil response helpers should return empty values")
	}

	resp := &http.Response{Header: make(http.Header)}
	resp.Header.Set(DefaultCacheStatusHeader, string(StatusHit))
	resp.Header.Set(DefaultCacheReasonHeader, string(ReasonCacheHit))

	if !IsCacheHit(resp) || !IsFromCache(resp) {
		t.Fatalf("expected hit helpers to be true")
	}
	if IsCacheMiss(resp) || IsCacheRevalidated(resp) {
		t.Fatalf("unexpected helper values for hit")
	}

	if !CacheStatusFromResponse(resp).IsValid() {
		t.Fatalf("expected cache status to be valid")
	}
	if !CacheReasonFromResponse(resp).IsValid() {
		t.Fatalf("expected cache reason to be valid")
	}
}

func TestCanonicalCacheKeyCredentialIsolation(t *testing.T) {
	t.Parallel()

	baseReq, _ := http.NewRequest(http.MethodGet, "http://example.com/resource", http.NoBody)
	keyBase := CanonicalCacheKey(baseReq, DefaultIgnoredCacheKeyHeaders)

	userReqA, _ := http.NewRequest(http.MethodGet, "http://alice:secret-a@example.com/resource", http.NoBody)
	userReqB, _ := http.NewRequest(http.MethodGet, "http://bob:secret-b@example.com/resource", http.NoBody)
	keyUserA := CanonicalCacheKey(userReqA, DefaultIgnoredCacheKeyHeaders)
	keyUserB := CanonicalCacheKey(userReqB, DefaultIgnoredCacheKeyHeaders)
	if keyUserA == keyUserB {
		t.Fatalf("expected URL credentials to produce distinct cache keys")
	}
	if keyBase == keyUserA {
		t.Fatalf("expected credentialed URL request to differ from non-credentialed key")
	}
	if strings.Contains(keyUserA, "secret-a") || strings.Contains(keyUserB, "secret-b") {
		t.Fatalf("expected URL credentials to be hashed in cache key")
	}

	authReqA, _ := http.NewRequest(http.MethodGet, "http://example.com/resource", http.NoBody)
	authReqA.Header.Set("Authorization", "Bearer token-a")
	authReqB, _ := http.NewRequest(http.MethodGet, "http://example.com/resource", http.NoBody)
	authReqB.Header.Set("Authorization", "Bearer token-b")
	keyAuthA := CanonicalCacheKey(authReqA, DefaultIgnoredCacheKeyHeaders)
	keyAuthB := CanonicalCacheKey(authReqB, DefaultIgnoredCacheKeyHeaders)
	if keyAuthA == keyAuthB {
		t.Fatalf("expected authorization header value changes to produce distinct cache keys")
	}
	if strings.Contains(keyAuthA, "token-a") || strings.Contains(keyAuthB, "token-b") {
		t.Fatalf("expected authorization credentials to be hashed in cache key")
	}

	cookieReqA, _ := http.NewRequest(http.MethodGet, "http://example.com/resource", http.NoBody)
	cookieReqA.Header.Set("Cookie", "session=aaa")
	cookieReqB, _ := http.NewRequest(http.MethodGet, "http://example.com/resource", http.NoBody)
	cookieReqB.Header.Set("Cookie", "session=bbb")
	if CanonicalCacheKey(cookieReqA, DefaultIgnoredCacheKeyHeaders) == CanonicalCacheKey(cookieReqB, DefaultIgnoredCacheKeyHeaders) {
		t.Fatalf("expected cookie changes to produce distinct cache keys")
	}

	acceptReqA, _ := http.NewRequest(http.MethodGet, "http://example.com/resource", http.NoBody)
	acceptReqA.Header.Set("Accept", "application/json")
	acceptReqB, _ := http.NewRequest(http.MethodGet, "http://example.com/resource", http.NoBody)
	acceptReqB.Header.Set("Accept", "text/plain")
	if CanonicalCacheKey(acceptReqA, DefaultIgnoredCacheKeyHeaders) == CanonicalCacheKey(acceptReqB, DefaultIgnoredCacheKeyHeaders) {
		t.Fatalf("expected non-ignored header changes to affect canonical key")
	}
}

func TestCanonicalCacheKeyDefaultIgnoredHeaders(t *testing.T) {
	t.Parallel()

	r1, _ := http.NewRequest(http.MethodGet, "http://example.com/resource", http.NoBody)
	r1.Header.Set("X-Request-ID", "req-1")
	r1.Header.Set("X-Trade-Id", "trade-1")
	r2, _ := http.NewRequest(http.MethodGet, "http://example.com/resource", http.NoBody)
	r2.Header.Set("X-Request-ID", "req-2")
	r2.Header.Set("X-Trade-Id", "trade-2")

	if CanonicalCacheKey(r1, DefaultIgnoredCacheKeyHeaders) != CanonicalCacheKey(r2, DefaultIgnoredCacheKeyHeaders) {
		t.Fatalf("expected default ignored request metadata headers to not affect key")
	}
}

func TestCanonicalCacheKeyWithIgnoredHeaders(t *testing.T) {
	t.Parallel()

	r1, _ := http.NewRequest(http.MethodGet, "http://example.com/resource", http.NoBody)
	r1.Header.Set("Authorization", "Bearer token-a")
	r2, _ := http.NewRequest(http.MethodGet, "http://example.com/resource", http.NoBody)
	r2.Header.Set("Authorization", "Bearer token-b")

	k1 := CanonicalCacheKey(r1, []string{"Authorization"})
	k2 := CanonicalCacheKey(r2, []string{"Authorization"})
	if k1 != k2 {
		t.Fatalf("expected explicitly ignored headers to be excluded from key")
	}
}

func TestTransportCustomCacheKeyFunc(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Cache-Control", "max-age=60")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&Config{
		Storage: NewMemoryStorage(128, time.Hour),
		CacheKeyFunc: func(req *http.Request, _ []string) string {
			return req.Method + " " + req.URL.Scheme + "://" + req.URL.Host + req.URL.Path
		},
	})

	req1, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/resource?one=1", http.NoBody)
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("request 1 failed: %v", err)
	}
	if !IsCacheMiss(resp1) {
		t.Fatalf("expected first request to miss, got %q", CacheStatusFromResponse(resp1))
	}
	_ = resp1.Body.Close()

	req2, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/resource?two=2", http.NoBody)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	if !IsCacheHit(resp2) {
		t.Fatalf("expected second request to hit due to custom key, got %q", CacheStatusFromResponse(resp2))
	}
	_ = resp2.Body.Close()

	if calls.Load() != 1 {
		t.Fatalf("expected one upstream call with custom key func, got %d", calls.Load())
	}
}

func TestTransportIgnoredCacheKeyHeadersConfig(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&Config{
		Storage:                NewMemoryStorage(128, time.Hour),
		IgnoredCacheKeyHeaders: []string{"Authorization"},
	})

	req1, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/resource", http.NoBody)
	req1.Header.Set("Authorization", "Bearer token-a")
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("request 1 failed: %v", err)
	}
	if !IsCacheMiss(resp1) {
		t.Fatalf("expected first request to miss, got %q", CacheStatusFromResponse(resp1))
	}
	_ = resp1.Body.Close()

	req2, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/resource", http.NoBody)
	req2.Header.Set("Authorization", "Bearer token-b")
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	if !IsCacheHit(resp2) {
		t.Fatalf("expected second request to hit when auth header is ignored, got %q", CacheStatusFromResponse(resp2))
	}
	_ = resp2.Body.Close()

	if calls.Load() != 1 {
		t.Fatalf("expected one upstream call with ignored auth header, got %d", calls.Load())
	}
}

func TestTransportCacheExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		base := &fakeRoundTripper{handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Cache-Control", "max-age=5")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})}

		tr := NewTransport(&Config{
			BaseTransport: base,
			Storage:       NewMemoryStorage(128, time.Hour),
		})

		req1, _ := http.NewRequest(http.MethodGet, "http://test.local/x", http.NoBody)
		resp1, err := tr.RoundTrip(req1)
		if err != nil {
			t.Fatalf("request 1 failed: %v", err)
		}
		if !IsCacheMiss(resp1) {
			t.Fatalf("expected miss on first request, got %q", CacheStatusFromResponse(resp1))
		}

		time.Sleep(3 * time.Second)

		req2, _ := http.NewRequest(http.MethodGet, "http://test.local/x", http.NoBody)
		resp2, err := tr.RoundTrip(req2)
		if err != nil {
			t.Fatalf("request 2 failed: %v", err)
		}
		if !IsCacheHit(resp2) {
			t.Fatalf("expected hit before expiry (3s < 5s max-age), got %q", CacheStatusFromResponse(resp2))
		}
		if calls.Load() != 1 {
			t.Fatalf("expected 1 upstream call before expiry, got %d", calls.Load())
		}

		time.Sleep(3 * time.Second)

		req3, _ := http.NewRequest(http.MethodGet, "http://test.local/x", http.NoBody)
		resp3, err := tr.RoundTrip(req3)
		if err != nil {
			t.Fatalf("request 3 failed: %v", err)
		}
		if IsCacheHit(resp3) {
			t.Fatalf("expected miss after expiry (6s > 5s max-age), got %q", CacheStatusFromResponse(resp3))
		}
		if calls.Load() != 2 {
			t.Fatalf("expected 2 upstream calls after expiry, got %d", calls.Load())
		}
	})
}

func TestTransportRevalidationWithFakeTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		base := &fakeRoundTripper{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := calls.Add(1)
			if n == 1 {
				w.Header().Set("ETag", `"v1"`)
				w.Header().Set("Cache-Control", "max-age=2")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("payload"))
				return
			}
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.Header().Set("Cache-Control", "max-age=10")
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("new-payload"))
		})}

		tr := NewTransport(&Config{
			BaseTransport: base,
			Storage:       NewMemoryStorage(128, time.Hour),
		})

		req1, _ := http.NewRequest(http.MethodGet, "http://test.local/reval", http.NoBody)
		resp1, err := tr.RoundTrip(req1)
		if err != nil {
			t.Fatalf("request 1 failed: %v", err)
		}
		_ = resp1.Body.Close()

		time.Sleep(1 * time.Second)

		req2, _ := http.NewRequest(http.MethodGet, "http://test.local/reval", http.NoBody)
		resp2, err := tr.RoundTrip(req2)
		if err != nil {
			t.Fatalf("request 2 failed: %v", err)
		}
		_ = resp2.Body.Close()
		if !IsCacheHit(resp2) {
			t.Fatalf("expected cache hit within max-age, got %q", CacheStatusFromResponse(resp2))
		}

		time.Sleep(2 * time.Second)

		req3, _ := http.NewRequest(http.MethodGet, "http://test.local/reval", http.NoBody)
		resp3, err := tr.RoundTrip(req3)
		if err != nil {
			t.Fatalf("request 3 failed: %v", err)
		}
		body, _ := io.ReadAll(resp3.Body)
		_ = resp3.Body.Close()
		if !IsCacheRevalidated(resp3) {
			t.Fatalf("expected revalidated after expiry, got %q", CacheStatusFromResponse(resp3))
		}
		if string(body) != "payload" {
			t.Fatalf("expected original payload from revalidation, got %q", body)
		}

		req4, _ := http.NewRequest(http.MethodGet, "http://test.local/reval", http.NoBody)
		resp4, err := tr.RoundTrip(req4)
		if err != nil {
			t.Fatalf("request 4 failed: %v", err)
		}
		_ = resp4.Body.Close()
		if !IsCacheHit(resp4) {
			t.Fatalf("expected hit after revalidation refreshed max-age, got %q", CacheStatusFromResponse(resp4))
		}
	})
}
