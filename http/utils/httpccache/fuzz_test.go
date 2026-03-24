package httpccache

import (
	"encoding/hex"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func FuzzCanonicalCacheKey(f *testing.F) {
	f.Add(http.MethodGet, "https://example.com/path?x=1", "Authorization", "Bearer abc", "X-Request-ID")
	f.Add(http.MethodGet, "https://alice:secret@example.com/path#frag", "Cookie", "session=xyz", "X-Request-ID,X-Trade-ID")
	f.Add(http.MethodHead, "http://example.net/a/b?c=d", "Accept", "application/json", "")

	f.Fuzz(func(t *testing.T, method, rawURL, headerKey, headerVal, ignoredCSV string) {
		if method == "" {
			method = http.MethodGet
		}
		req, err := http.NewRequest(method, rawURL, http.NoBody)
		if err != nil {
			return
		}
		if headerKey != "" {
			req.Header.Add(headerKey, headerVal)
		}

		var ignored []string
		for _, item := range strings.Split(ignoredCSV, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				ignored = append(ignored, item)
			}
		}

		key1 := CanonicalCacheKey(req, ignored)
		key2 := CanonicalCacheKey(req, ignored)
		if key1 != key2 {
			t.Fatalf("canonical key not deterministic: %q != %q", key1, key2)
		}

		clone := req.Clone(req.Context())
		clone.URL.Fragment = "changed-fragment"
		if CanonicalCacheKey(clone, ignored) != key1 {
			t.Fatalf("fragment should not affect key")
		}

		if req.URL.User != nil {
			username := req.URL.User.Username()
			password, _ := req.URL.User.Password()
			if username != "" && strings.Contains(key1, username) {
				t.Fatalf("username leaked in canonical key")
			}
			if password != "" && strings.Contains(key1, password) {
				t.Fatalf("password leaked in canonical key")
			}
		}
	})
}

func FuzzParseCacheControl(f *testing.F) {
	f.Add("max-age=60, no-cache")
	f.Add(`public, s-maxage=120, stale-while-revalidate="30"`)
	f.Add(",,,,,")

	f.Fuzz(func(t *testing.T, raw string) {
		directives := parseCacheControl([]string{raw, raw})
		for key := range directives {
			if key != strings.ToLower(key) {
				t.Fatalf("directive key must be lowercase: %q", key)
			}
		}
	})
}

func FuzzVaryHandling(f *testing.F) {
	f.Add("Accept-Encoding, Accept-Language", "Accept-Encoding", "gzip", "br")
	f.Add("*", "X-Test", "a", "b")
	f.Add("X-Custom", "X-Custom", "one", "two")

	f.Fuzz(func(t *testing.T, varyRaw, key, v1, v2 string) {
		if key == "" {
			key = "X-Test"
		}

		resp := &http.Response{Header: make(http.Header)}
		if varyRaw != "" {
			resp.Header.Set("Vary", varyRaw)
		}
		vary := parseVary(resp)
		for _, item := range vary {
			if item == "" {
				t.Fatalf("vary key should not be empty")
			}
		}

		req1, err := http.NewRequest(http.MethodGet, "https://example.com/resource", http.NoBody)
		if err != nil {
			t.Fatalf("failed to build request: %v", err)
		}
		req1.Header.Set(key, v1)

		req2 := req1.Clone(req1.Context())
		req2.Header.Set(key, v2)

		entry := &CacheEntry{
			ResponseHeader: resp.Header.Clone(),
			VaryValues:     map[string]string{},
		}
		for _, vk := range vary {
			entry.VaryValues[vk] = strings.Join(req1.Header.Values(vk), ",")
		}

		if !strings.Contains(varyRaw, "*") && !varyAllowsRequest(entry, req1) {
			t.Fatalf("entry built from req1 should match req1")
		}

		_ = varyAllowsRequest(entry, req2)
	})
}

func FuzzParseCacheFilename(f *testing.F) {
	f.Add("7e647603e9857beb5d3e9545cc0b0610319aa1ce1255e4c365db2e451bc2b8b3.v1.cache")
	f.Add("invalid.cache")
	f.Add("")

	f.Fuzz(func(t *testing.T, name string) {
		hash, version, ok := parseCacheFilename(name)
		if !ok {
			return
		}
		if len(hash) != 64 {
			t.Fatalf("expected sha256 hex hash length, got %d", len(hash))
		}
		if _, err := hex.DecodeString(hash); err != nil {
			t.Fatalf("hash should be valid hex: %v", err)
		}
		if version <= 0 {
			t.Fatalf("expected positive version, got %d", version)
		}
	})
}

func FuzzUnmarshalCacheEntryJSON(f *testing.F) {
	f.Add(`{"key":"k","method":"GET","url":"https://example.com","created_at":"2026-01-01T00:00:00Z","stored_at":"2026-01-01T00:00:00Z","expires_at":"2026-01-01T00:00:10Z","response_status":200,"response_header":{"Cache-Control":["max-age=10"]},"vary_values":{"Accept":"application/json"}}`)
	f.Add("{}")
	f.Add("{")

	f.Fuzz(func(t *testing.T, raw string) {
		entry, err := unmarshalCacheEntryJSON([]byte(raw))
		if err == nil && entry == nil {
			t.Fatalf("nil entry with nil error")
		}
	})
}

func FuzzParseSecondsDirective(f *testing.F) {
	f.Add("max-age", "60")
	f.Add("s-maxage", "0")
	f.Add("max-age", "-1")
	f.Add("max-age", "abc")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, key, value string) {
		directives := map[string]string{key: value}
		d, ok := parseSecondsDirective(directives, key)

		parsed, err := strconv.Atoi(value)
		expectOK := err == nil && parsed >= 0
		if ok != expectOK {
			t.Fatalf("parseSecondsDirective ok mismatch: got=%v expect=%v value=%q", ok, expectOK, value)
		}
		if ok {
			maxSeconds := int64(math.MaxInt64 / int64(time.Second))
			if int64(parsed) <= maxSeconds {
				expectedDuration := time.Duration(parsed) * time.Second
				if d != expectedDuration {
					t.Fatalf("duration mismatch: got=%v expect=%v", d, expectedDuration)
				}
			}
		}
	})
}

func FuzzEvaluateResponsePolicy(f *testing.F) {
	f.Add(http.MethodGet, 200, "max-age=60", "", "", true, true)
	f.Add(http.MethodGet, 200, "public, max-age=60", "Bearer abc", "", false, false)
	f.Add(http.MethodPost, 200, "max-age=60", "", "", true, false)
	f.Add(http.MethodGet, 500, "", "", "", false, false)

	f.Fuzz(func(t *testing.T, method string, statusCode int, cacheControl, authorization, expires string, allowAuthCache, allowHeuristic bool) {
		if method == "" {
			method = http.MethodGet
		}
		req, err := http.NewRequest(method, "https://example.com/resource", http.NoBody)
		if err != nil {
			return
		}
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}

		resp := &http.Response{
			StatusCode: statusCode,
			Header:     make(http.Header),
			Request:    req,
		}
		if cacheControl != "" {
			resp.Header.Set("Cache-Control", cacheControl)
		}
		if expires != "" {
			resp.Header.Set("Expires", expires)
		}

		policy := evaluateResponsePolicy(req, resp, allowAuthCache, allowHeuristic)
		if policy.cacheable {
			if policy.reason != ReasonStored {
				t.Fatalf("cacheable responses must use reason=%q, got=%q", ReasonStored, policy.reason)
			}
			if policy.expiresAt.IsZero() {
				t.Fatalf("cacheable policy must set expiresAt")
			}
		}
		if policy.reason == "" {
			t.Fatalf("policy reason should always be set")
		}
	})
}
