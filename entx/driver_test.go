// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package entx

import (
	"bytes"
	"context"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
)

func TestSQLitePragmaValues(t *testing.T) {
	t.Parallel()

	got := sqlitePragmaValues(Pragma{"foreign_keys", "ON"}, Pragma{"busy_timeout", "30000"})
	want := url.Values{
		"_pragma": {"foreign_keys(ON)", "busy_timeout(30000)"},
		"_txlock": {"immediate"},
	}

	if !slices.Equal(got["_pragma"], want["_pragma"]) {
		t.Errorf("_pragma = %v, want %v", got["_pragma"], want["_pragma"])
	}
	if got.Get("_txlock") != want.Get("_txlock") {
		t.Errorf("_txlock = %q, want %q", got.Get("_txlock"), want.Get("_txlock"))
	}
}

func TestConstrainDialect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scheme  string
		allowed []string
		wantErr bool
	}{
		{
			name:    "sqlite accepts sqlite",
			scheme:  "sqlite",
			allowed: []string{dialect.SQLite},
		},
		{
			name:    "file accepts sqlite",
			scheme:  "file",
			allowed: []string{dialect.SQLite},
		},
		{
			name:    "postgres accepts postgres",
			scheme:  dialect.Postgres,
			allowed: []string{dialect.Postgres},
		},
		{
			name:    "sqlite rejects postgres",
			scheme:  "sqlite",
			allowed: []string{dialect.Postgres},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ConstrainDialect(&url.URL{Scheme: tt.scheme}, tt.allowed...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ConstrainDialect() error = %v, want error: %t", err, tt.wantErr)
			}
		})
	}
}

func TestDriver(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	driver, err := Driver(context.Background(), logger, &url.URL{
		Scheme: dialect.Postgres,
		Host:   "localhost:5432",
		Path:   "/entx_test",
		User:   url.UserPassword("entx", "entx"),
	})
	if err != nil {
		t.Fatalf("Driver() error = %v", err)
	}
	defer func() {
		if closeErr := driver.Close(); closeErr != nil {
			t.Errorf("driver.Close() error = %v", closeErr)
		}
	}()

	if got := driver.Dialect(); got != dialect.Postgres {
		t.Errorf("Driver().Dialect() = %q, want %q", got, dialect.Postgres)
	}
	if !strings.Contains(logs.String(), "opening database") {
		t.Errorf("custom logger output = %q, want opening database message", logs.String())
	}
}

func TestDriver_defaultsLogger(t *testing.T) {
	t.Parallel()

	driver, err := Driver(context.Background(), nil, &url.URL{
		Scheme: dialect.Postgres,
		Host:   "localhost:5432",
		Path:   "/entx_test",
		User:   url.UserPassword("entx", "entx"),
	})
	if err != nil {
		t.Fatalf("Driver() error = %v", err)
	}
	if closeErr := driver.Close(); closeErr != nil {
		t.Fatalf("driver.Close() error = %v", closeErr)
	}
}

func TestMemoryDriver_usesMemoryDatabase(t *testing.T) {
	t.Parallel()

	driver := MemoryDriver(slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := driver.Close(); err != nil {
			t.Errorf("driver.Close() error = %v", err)
		}
	})

	var sequence int
	var name, filename string
	err := driver.DB().QueryRowContext(
		context.Background(),
		"PRAGMA database_list",
	).Scan(&sequence, &name, &filename)
	if err != nil {
		t.Fatalf("querying database_list: %v", err)
	}
	if filename != "" {
		t.Fatalf("database filename = %q, want an in-memory database", filename)
	}
}

func TestDriver_supportsExampleDSNs(t *testing.T) {
	tests := []struct {
		name     string
		rawDSN   string
		dialect  string
		filePath string
		memory   bool
	}{
		{
			name:     "sqlite relative path",
			rawDSN:   "sqlite://./some/local/path/data.sqlite",
			dialect:  dialect.SQLite,
			filePath: "some/local/path/data.sqlite",
		},
		{
			name:     "file relative path",
			rawDSN:   "file://some/local/path/data.db",
			dialect:  dialect.SQLite,
			filePath: "some/local/path/data.db",
		},
		{
			name:    "sqlite memory",
			rawDSN:  "sqlite::memory:",
			dialect: dialect.SQLite,
			memory:  true,
		},
		{
			name:    "postgres",
			rawDSN:  "postgres://user:pass@host:5432/db",
			dialect: dialect.Postgres,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			dsn, err := url.Parse(tt.rawDSN)
			if err != nil {
				t.Fatalf("parsing %q: %v", tt.rawDSN, err)
			}
			if tt.filePath != "" {
				if mkdirErr := os.MkdirAll(filepath.Dir(tt.filePath), 0o755); mkdirErr != nil {
					t.Fatalf("creating database directory: %v", mkdirErr)
				}
			}

			driver, err := Driver(context.Background(), slog.New(slog.DiscardHandler), dsn)
			if err != nil {
				t.Fatalf("Driver() error = %v", err)
			}
			t.Cleanup(func() {
				if closeErr := driver.Close(); closeErr != nil {
					t.Errorf("driver.Close() error = %v", closeErr)
				}
			})

			if got := driver.Dialect(); got != tt.dialect {
				t.Fatalf("Driver().Dialect() = %q, want %q", got, tt.dialect)
			}
			if tt.memory {
				var sequence int
				var name, filename string
				memoryErr := driver.DB().QueryRowContext(
					context.Background(),
					"PRAGMA database_list",
				).Scan(&sequence, &name, &filename)
				if memoryErr != nil {
					t.Fatalf("querying database_list: %v", memoryErr)
				}
				if filename != "" {
					t.Fatalf("database filename = %q, want an in-memory database", filename)
				}
				return
			}
			if tt.filePath == "" {
				return
			}
			if _, queryErr := driver.DB().ExecContext(
				context.Background(),
				"CREATE TABLE test (id INTEGER PRIMARY KEY)",
			); queryErr != nil {
				t.Fatalf("creating table: %v", queryErr)
			}
			if _, statErr := os.Stat(tt.filePath); statErr != nil {
				t.Fatalf("SQLite database file was not created: %v", statErr)
			}
		})
	}
}

func TestDriver_rejectsNilDSN(t *testing.T) {
	t.Parallel()

	if _, err := Driver(context.Background(), nil, nil); err == nil {
		t.Fatal("Driver(nil) returned nil error")
	}
}

func TestDriver_rejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()

	if _, err := Driver(context.Background(), nil, &url.URL{Scheme: "unknown"}); err == nil {
		t.Fatal("Driver() returned nil error for unsupported scheme")
	}
}

func TestMustDriver_panicsOnError(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("MustDriver() did not panic")
		}
	}()
	MustDriver(context.Background(), nil, nil)
}
