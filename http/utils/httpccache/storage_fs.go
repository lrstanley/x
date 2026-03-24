// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpccache

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const fileStorageVersion = 1

// FileStorage stores cache entries on disk as:
//
//	<metadata-json>\n<body-bytes>
//
// Filenames are opaque and versioned.
type FileStorage struct {
	Dir        string
	MaxEntries int
	MaxAge     time.Duration

	mu sync.Mutex
}

// NewFileStorage creates a new filesystem storage backend.
func NewFileStorage(dir string, maxEntries int, maxAge time.Duration) (*FileStorage, error) {
	if dir == "" {
		return nil, errors.New("dir cannot be empty")
	}

	st := &FileStorage{
		Dir:        dir,
		MaxEntries: maxEntries,
		MaxAge:     maxAge,
	}
	if err := st.ensureDir(); err != nil {
		return nil, err
	}
	return st, nil
}

// Get retrieves a record by key from filesystem storage.
func (f *FileStorage) Get(_ context.Context, key string) (*CacheEntry, io.ReadCloser, error) {
	f.mu.Lock()
	path := f.pathForKey(key)
	file, err := os.Open(path)
	if err != nil {
		f.mu.Unlock()
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}

	if !f.filePathMatchesVersion(path) {
		_ = file.Close()
		f.mu.Unlock()
		return nil, nil, ErrNotFound
	}

	reader := bufio.NewReaderSize(file, 64*1024)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		_ = file.Close()
		f.mu.Unlock()
		_ = os.Remove(path)
		return nil, nil, ErrNotFound
	}

	entry, err := unmarshalCacheEntryJSON(bytes.TrimRight(line, "\r\n"))
	if err != nil {
		_ = file.Close()
		f.mu.Unlock()
		_ = os.Remove(path)
		return nil, nil, ErrNotFound
	}

	now := time.Now()
	if f.MaxAge > 0 && now.Sub(entry.CreatedAt) > f.MaxAge {
		_ = file.Close()
		f.mu.Unlock()
		_ = os.Remove(path)
		return nil, nil, ErrNotFound
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		f.mu.Unlock()
		return nil, nil, err
	}

	bodyOffset := int64(len(line))
	bodySize := info.Size() - bodyOffset
	if bodySize < 0 {
		_ = file.Close()
		f.mu.Unlock()
		_ = os.Remove(path)
		return nil, nil, ErrNotFound
	}

	entry.BodySize = bodySize
	if bodySize == 0 {
		_ = file.Close()
		f.mu.Unlock()
		return entry, nil, nil
	}

	section := io.NewSectionReader(file, bodyOffset, bodySize)
	body := &fileReadCloser{Reader: section, file: file}
	f.mu.Unlock()
	return entry, body, nil
}

// Set writes a record by key to filesystem storage.
func (f *FileStorage) Set(_ context.Context, key string, entry *CacheEntry, body io.Reader) error {
	if entry == nil {
		return errors.New("entry cannot be nil")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.ensureDir(); err != nil {
		return err
	}

	if body == nil {
		return f.setMetaOnlyLocked(key, entry)
	}

	entryToStore := cloneCacheEntry(entry)
	entryToStore.Key = key
	entryToStore.BodySize = 0

	meta, err := json.Marshal(entryToStore)
	if err != nil {
		return err
	}

	path := f.pathForKey(key)
	tmpPath, err := f.tempPath(path)
	if err != nil {
		return err
	}

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec
	if err != nil {
		return err
	}

	if _, err := out.Write(meta); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := out.Write([]byte{'\n'}); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := io.Copy(out, body); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return f.pruneLocked()
}

// Delete removes a cached file for a key.
func (f *FileStorage) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	err := os.Remove(f.pathForKey(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Prune removes stale entries, evicts by MaxEntries, and purges mismatched
// storage versions.
func (f *FileStorage) Prune(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pruneLocked()
}

func (f *FileStorage) ensureDir() error {
	return os.MkdirAll(f.Dir, 0o700)
}

func (f *FileStorage) setMetaOnlyLocked(key string, entry *CacheEntry) error {
	path := f.pathForKey(key)
	in, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	defer in.Close()

	reader := bufio.NewReaderSize(in, 64*1024)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return err
	}

	info, err := in.Stat()
	if err != nil {
		return err
	}

	bodyOffset := int64(len(line))
	bodySize := info.Size() - bodyOffset
	if bodySize < 0 {
		return ErrNotFound
	}

	entryToStore := cloneCacheEntry(entry)
	entryToStore.Key = key
	entryToStore.BodySize = 0
	meta, err := json.Marshal(entryToStore)
	if err != nil {
		return err
	}

	tmpPath, err := f.tempPath(path)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec
	if err != nil {
		return err
	}

	if _, err := out.Write(meta); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := out.Write([]byte{'\n'}); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return err
	}

	if bodySize > 0 {
		if _, err := in.Seek(bodyOffset, io.SeekStart); err != nil {
			_ = out.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		if _, err := io.CopyN(out, in, bodySize); err != nil {
			_ = out.Close()
			_ = os.Remove(tmpPath)
			return err
		}
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return f.pruneLocked()
}

func (f *FileStorage) pathForKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])
	filename := f.cacheFilename(hash, fileStorageVersion)
	return filepath.Join(f.Dir, filename)
}

func (f *FileStorage) tempPath(path string) (string, error) {
	tmp, err := os.CreateTemp(f.Dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

func (f *FileStorage) cacheFilename(hash string, version int) string {
	return hash + ".v" + strconv.Itoa(version) + ".cache"
}

func (f *FileStorage) filePathMatchesVersion(path string) bool {
	_, version, ok := parseCacheFilename(filepath.Base(path))
	return ok && version == fileStorageVersion
}

func parseCacheFilename(name string) (hash string, version int, ok bool) {
	if !strings.HasSuffix(name, ".cache") {
		return "", 0, false
	}

	base := strings.TrimSuffix(name, ".cache")
	idx := strings.LastIndex(base, ".v")
	if idx <= 0 || idx == len(base)-2 {
		return "", 0, false
	}

	hashPart := base[:idx]
	versionPart := base[idx+2:]
	if len(hashPart) != sha256.Size*2 {
		return "", 0, false
	}

	if _, err := hex.DecodeString(hashPart); err != nil {
		return "", 0, false
	}

	parsedVersion, err := strconv.Atoi(versionPart)
	if err != nil || parsedVersion <= 0 {
		return "", 0, false
	}

	return hashPart, parsedVersion, true
}

func readFileCreatedAt(path string) (time.Time, bool) {
	in, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer in.Close()

	reader := bufio.NewReaderSize(in, 64*1024)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return time.Time{}, false
	}

	entry, err := unmarshalCacheEntryJSON(bytes.TrimRight(line, "\r\n"))
	if err != nil {
		return time.Time{}, false
	}
	return entry.CreatedAt, true
}

func (f *FileStorage) pruneLocked() error {
	entries, err := os.ReadDir(f.Dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	now := time.Now()
	type pruneItem struct {
		path      string
		createdAt time.Time
	}
	items := make([]pruneItem, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".cache") {
			continue
		}

		path := filepath.Join(f.Dir, name)
		_, version, ok := parseCacheFilename(name)
		if !ok || version != fileStorageVersion {
			_ = os.Remove(path)
			continue
		}

		createdAt, ok := readFileCreatedAt(path)
		if !ok {
			_ = os.Remove(path)
			continue
		}

		if f.MaxAge > 0 && now.Sub(createdAt) > f.MaxAge {
			_ = os.Remove(path)
			continue
		}

		items = append(items, pruneItem{path: path, createdAt: createdAt})
	}

	if f.MaxEntries <= 0 || len(items) <= f.MaxEntries {
		return nil
	}

	slices.SortFunc(items, func(a, b pruneItem) int {
		if a.createdAt.Before(b.createdAt) {
			return -1
		}
		if a.createdAt.After(b.createdAt) {
			return 1
		}
		return strings.Compare(a.path, b.path)
	})

	for len(items) > f.MaxEntries {
		_ = os.Remove(items[0].path)
		items = items[1:]
	}

	return nil
}

type fileReadCloser struct {
	io.Reader
	file *os.File
}

func (f *fileReadCloser) Close() error {
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	f.Reader = nil
	return err
}
