package httpccache

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestMemoryStorageRoundTripAndMetaOnlySet(t *testing.T) {
	t.Parallel()

	st := NewMemoryStorage(10, time.Hour)
	key := "GET http://x/a"
	entry := &CacheEntry{
		Key:            key,
		Method:         http.MethodGet,
		URL:            "http://x/a",
		StoredAt:       time.Now().UTC(),
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
		CreatedAt:      time.Now().UTC(),
		ResponseStatus: http.StatusOK,
		ResponseHeader: http.Header{"ETag": []string{`"v1"`}},
		VaryValues:     map[string]string{"Accept": "application/json"},
	}

	if err := st.Set(context.Background(), key, entry, strings.NewReader("body-1")); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	gotEntry, gotBodyRC, err := st.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	gotBody, err := io.ReadAll(gotBodyRC)
	_ = gotBodyRC.Close()
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if string(gotBody) != "body-1" {
		t.Fatalf("unexpected body: %q", gotBody)
	}
	if gotEntry.BodySize != int64(len("body-1")) {
		t.Fatalf("unexpected body size: %d", gotEntry.BodySize)
	}

	updated := cloneCacheEntry(entry)
	updated.ResponseHeader.Set("ETag", `"v2"`)
	updated.ExpiresAt = updated.ExpiresAt.Add(time.Hour)
	updated.CreatedAt = updated.CreatedAt.Add(2 * time.Second)

	if err := st.Set(context.Background(), key, updated, nil); err != nil {
		t.Fatalf("set meta-only failed: %v", err)
	}

	gotUpdated, gotBodyRC, err := st.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get after meta-only failed: %v", err)
	}
	gotBody, err = io.ReadAll(gotBodyRC)
	_ = gotBodyRC.Close()
	if err != nil {
		t.Fatalf("read body after meta-only failed: %v", err)
	}
	if string(gotBody) != "body-1" {
		t.Fatalf("expected preserved body bytes, got %q", gotBody)
	}
	if gotUpdated.ResponseHeader.Get("ETag") != `"v2"` {
		t.Fatalf("expected updated metadata, got etag=%q", gotUpdated.ResponseHeader.Get("ETag"))
	}
}

func TestMemoryStorageSetMetaOnlyMissingEntry(t *testing.T) {
	t.Parallel()

	st := NewMemoryStorage(10, time.Hour)
	err := st.Set(context.Background(), "missing", &CacheEntry{
		Key:            "missing",
		Method:         http.MethodGet,
		URL:            "http://x/missing",
		StoredAt:       time.Now(),
		ExpiresAt:      time.Now().Add(time.Hour),
		CreatedAt:      time.Now(),
		ResponseStatus: http.StatusOK,
		ResponseHeader: http.Header{},
	}, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryStoragePruneMaxEntries(t *testing.T) {
	t.Parallel()

	st := NewMemoryStorage(2, 0)
	now := time.Now().UTC()
	for i := range 3 {
		key := "GET http://x/" + string(rune('a'+i))
		entry := &CacheEntry{
			Key:            key,
			Method:         http.MethodGet,
			URL:            "http://x/" + string(rune('a'+i)),
			StoredAt:       now.Add(time.Duration(i) * time.Second),
			ExpiresAt:      now.Add(time.Hour),
			CreatedAt:      now.Add(time.Duration(i) * time.Second),
			ResponseStatus: http.StatusOK,
			ResponseHeader: http.Header{},
		}
		if err := st.Set(context.Background(), key, entry, strings.NewReader("x")); err != nil {
			t.Fatalf("set %d failed: %v", i, err)
		}
	}

	_, _, err := st.Get(context.Background(), "GET http://x/a")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected oldest key to be evicted, got %v", err)
	}
}

func TestMemoryStoragePruneMaxAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := NewMemoryStorage(10, 10*time.Millisecond)
		entry := &CacheEntry{
			Key:            "GET http://x/age",
			Method:         http.MethodGet,
			URL:            "http://x/age",
			StoredAt:       time.Now(),
			ExpiresAt:      time.Now().Add(time.Hour),
			CreatedAt:      time.Now(),
			ResponseStatus: http.StatusOK,
			ResponseHeader: http.Header{},
		}

		if err := st.Set(context.Background(), entry.Key, entry, strings.NewReader("x")); err != nil {
			t.Fatalf("set failed: %v", err)
		}

		time.Sleep(20 * time.Millisecond)
		_, _, err := st.Get(context.Background(), entry.Key)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound after max age, got %v", err)
		}
	})
}
