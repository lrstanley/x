package httpccache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileStorageVersionedHashFilenameAndPerms(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st, err := NewFileStorage(dir, 10, time.Hour)
	if err != nil {
		t.Fatalf("new file storage failed: %v", err)
	}

	key := "GET https://example.com/very/secret/path?token=abc123"
	entry := testCacheEntry(key, time.Now().UTC())
	if err := st.Set(context.Background(), key, entry, strings.NewReader("ok")); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one cache file, got %d", len(entries))
	}

	name := entries[0].Name()
	if strings.Contains(name, "example.com") || strings.Contains(name, "secret") {
		t.Fatalf("cache filename should not leak key data: %q", name)
	}
	if !strings.HasSuffix(name, ".v1.cache") {
		t.Fatalf("expected versioned cache filename, got %q", name)
	}

	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected file mode 0600, got %o", info.Mode().Perm())
	}
}

func TestFileStorageWireFormatAndBodySectionReader(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st, err := NewFileStorage(dir, 10, time.Hour)
	if err != nil {
		t.Fatalf("new file storage failed: %v", err)
	}

	key := "GET https://example.com/large"
	entry := testCacheEntry(key, time.Now().UTC().Truncate(time.Second))
	body := bytes.Repeat([]byte("z"), 64*1024)

	if err := st.Set(context.Background(), key, entry, bytes.NewReader(body)); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	path := st.pathForKey(key)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}

	nl := bytes.IndexByte(raw, '\n')
	if nl < 0 {
		t.Fatal("expected newline after metadata json")
	}

	var onDisk CacheEntry
	if err := json.Unmarshal(raw[:nl], &onDisk); err != nil {
		t.Fatalf("metadata json parse failed: %v", err)
	}
	if onDisk.URL != entry.URL {
		t.Fatalf("unexpected metadata content: %q", onDisk.URL)
	}
	if !bytes.Equal(raw[nl+1:], body) {
		t.Fatalf("body bytes mismatch")
	}

	gotEntry, bodyRC, err := st.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	gotBody, err := io.ReadAll(bodyRC)
	_ = bodyRC.Close()
	if err != nil {
		t.Fatalf("body read failed: %v", err)
	}
	if gotEntry.BodySize != int64(len(body)) {
		t.Fatalf("expected body size %d, got %d", len(body), gotEntry.BodySize)
	}
	if !bytes.Equal(gotBody, body) {
		t.Fatalf("unexpected body bytes")
	}
}

func TestFileStorageMetaOnlySetPreservesBody(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st, err := NewFileStorage(dir, 10, time.Hour)
	if err != nil {
		t.Fatalf("new file storage failed: %v", err)
	}

	key := "GET https://example.com/meta-only"
	entry := testCacheEntry(key, time.Now().UTC())
	if err := st.Set(context.Background(), key, entry, strings.NewReader("body-1")); err != nil {
		t.Fatalf("initial set failed: %v", err)
	}

	updated := testCacheEntry(key, time.Now().UTC().Add(time.Minute))
	updated.ResponseHeader.Set("ETag", `"v2"`)
	if err := st.Set(context.Background(), key, updated, nil); err != nil {
		t.Fatalf("meta-only set failed: %v", err)
	}

	gotEntry, bodyRC, err := st.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	body, err := io.ReadAll(bodyRC)
	_ = bodyRC.Close()
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if string(body) != "body-1" {
		t.Fatalf("expected preserved body, got %q", body)
	}
	if gotEntry.ResponseHeader.Get("ETag") != `"v2"` {
		t.Fatalf("expected updated metadata etag, got %q", gotEntry.ResponseHeader.Get("ETag"))
	}
}

func TestFileStorageGetIgnoresDifferentVersionAndPrunePurges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st, err := NewFileStorage(dir, 10, time.Hour)
	if err != nil {
		t.Fatalf("new file storage failed: %v", err)
	}

	key := "GET https://example.com/only-old-version"
	now := time.Now().UTC()
	entry := testCacheEntry(key, now)
	meta, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])
	oldPath := filepath.Join(dir, st.cacheFilename(hash, fileStorageVersion+1))
	if err := os.WriteFile(oldPath, append(append([]byte(nil), meta...), '\n'), 0o600); err != nil {
		t.Fatalf("write old-version file failed: %v", err)
	}

	_, _, err = st.Get(context.Background(), key)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for different version file, got %v", err)
	}

	if err := st.Prune(context.Background()); err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected old-version file to be removed, stat err=%v", err)
	}
}

func testCacheEntry(key string, createdAt time.Time) *CacheEntry {
	return &CacheEntry{
		Key:            key,
		Method:         http.MethodGet,
		URL:            strings.TrimPrefix(key, "GET "),
		StoredAt:       createdAt,
		ExpiresAt:      createdAt.Add(time.Hour),
		CreatedAt:      createdAt,
		ResponseStatus: http.StatusOK,
		ResponseHeader: http.Header{"ETag": []string{`"v1"`}},
		VaryValues:     map[string]string{"Accept": "application/json"},
	}
}
