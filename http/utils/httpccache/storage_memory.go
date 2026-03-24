// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpccache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"time"
)

// MemoryStorage is an in-memory implementation of [Storage].
type MemoryStorage struct {
	MaxEntries int
	MaxAge     time.Duration

	mu      sync.Mutex
	records map[string]*memRecord
}

type memRecord struct {
	entry *CacheEntry
	body  []byte
}

// NewMemoryStorage creates a new in-memory storage backend.
func NewMemoryStorage(maxEntries int, maxAge time.Duration) *MemoryStorage {
	return &MemoryStorage{
		MaxEntries: maxEntries,
		MaxAge:     maxAge,
		records:    make(map[string]*memRecord),
	}
}

// Get retrieves a record by key from in-memory storage.
func (m *MemoryStorage) Get(_ context.Context, key string) (*CacheEntry, io.ReadCloser, error) {
	now := time.Now()

	m.mu.Lock()
	rec, ok := m.records[key]
	if !ok {
		m.mu.Unlock()
		return nil, nil, ErrNotFound
	}

	if m.MaxAge > 0 && now.Sub(rec.entry.CreatedAt) > m.MaxAge {
		delete(m.records, key)
		m.mu.Unlock()
		return nil, nil, ErrNotFound
	}

	entry := cloneCacheEntry(rec.entry)
	body := append([]byte(nil), rec.body...)
	m.mu.Unlock()

	entry.BodySize = int64(len(body))
	if len(body) == 0 {
		return entry, nil, nil
	}
	return entry, io.NopCloser(bytes.NewReader(body)), nil
}

// Set stores a record in memory and prunes old entries.
func (m *MemoryStorage) Set(_ context.Context, key string, entry *CacheEntry, body io.Reader) error {
	if entry == nil {
		return errors.New("entry cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if body == nil {
		return m.setMetaOnlyLocked(key, entry)
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	recordEntry := cloneCacheEntry(entry)
	recordEntry.Key = key
	recordEntry.BodySize = 0
	m.records[key] = &memRecord{
		entry: recordEntry,
		body:  data,
	}

	m.pruneLocked()
	return nil
}

func (m *MemoryStorage) setMetaOnlyLocked(key string, entry *CacheEntry) error {
	prev, ok := m.records[key]
	if !ok {
		return ErrNotFound
	}

	recordEntry := cloneCacheEntry(entry)
	recordEntry.Key = key
	recordEntry.BodySize = 0
	m.records[key] = &memRecord{
		entry: recordEntry,
		body:  prev.body,
	}

	m.pruneLocked()
	return nil
}

// Delete removes a record by key.
func (m *MemoryStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.records, key)
	m.mu.Unlock()
	return nil
}

// Prune deletes expired entries and evicts old entries beyond max capacity.
func (m *MemoryStorage) Prune(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	return nil
}

// pruneLocked performs pruning while the storage mutex is held.
func (m *MemoryStorage) pruneLocked() {
	now := time.Now()
	for key, record := range m.records {
		if m.MaxAge > 0 && now.Sub(record.entry.CreatedAt) > m.MaxAge {
			delete(m.records, key)
		}
	}

	if m.MaxEntries <= 0 || len(m.records) <= m.MaxEntries {
		return
	}

	type item struct {
		key       string
		createdAt time.Time
	}

	items := make([]item, 0, len(m.records))
	for key, record := range m.records {
		items = append(items, item{key: key, createdAt: record.entry.CreatedAt})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].createdAt.Before(items[j].createdAt)
	})

	for len(m.records) > m.MaxEntries {
		victim := items[0]
		items = items[1:]
		delete(m.records, victim.key)
	}
}

func cloneCacheEntry(src *CacheEntry) *CacheEntry {
	if src == nil {
		return nil
	}

	dst := *src
	if src.ResponseHeader != nil {
		dst.ResponseHeader = src.ResponseHeader.Clone()
	} else {
		dst.ResponseHeader = make(map[string][]string)
	}

	if src.VaryValues != nil {
		dst.VaryValues = make(map[string]string, len(src.VaryValues))
		for key, value := range src.VaryValues {
			dst.VaryValues[key] = value
		}
	} else {
		dst.VaryValues = make(map[string]string)
	}

	return &dst
}
