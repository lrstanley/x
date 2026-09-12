// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package entx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"sync"
	"time"
	"uuid"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	sqlite "modernc.org/sqlite"
)

var sqliteInit = sync.OnceFunc(func() {
	sql.Register(dialect.SQLite, &sqlite.Driver{})
})

// Pragma is a SQL pragma.
type Pragma [2]string

// DefaultSQLitePragmas are the default SQLite pragmas used when no pragmas are provided.
var DefaultSQLitePragmas = []Pragma{
	{"foreign_keys", "ON"},
	{"journal_mode", "WAL"},
	{"synchronous", "NORMAL"},
	{"busy_timeout", "30000"},
	{"temp_store", "MEMORY"},
}

func sqlitePragmaValues(pragmas ...Pragma) url.Values {
	params := url.Values{}
	for _, p := range pragmas {
		params.Add("_pragma", p[0]+"("+p[1]+")")
	}
	params.Set("_txlock", "immediate")
	return params
}

// ConstrainDialect constrains the dialect of the given DSN to the given dialects,
// e.g. [dialect.SQLite] or [dialect.Postgres].
func ConstrainDialect(dsn *url.URL, dialects ...string) error {
	if slices.Contains(dialects, dialect.SQLite) {
		dialects = append(dialects, "sqlite", "file")
	}

	if !slices.Contains(dialects, dsn.Scheme) {
		return fmt.Errorf("unsupported dialect: %s", dsn.Scheme)
	}
	return nil
}

// MemoryDriver returns a new SQLite driver that uses an in-memory database. This is
// a shortcut for [Driver] with the following URL: "sqlite::memory:".
func MemoryDriver(logger *slog.Logger) *entsql.Driver {
	return MustDriver(context.Background(), logger, &url.URL{
		Scheme: "sqlite",   //nolint:goconst
		Opaque: ":memory:", //nolint:goconst
	})
}

// MustDriver is a shortcut for [Driver] that panics if the driver creation fails.
func MustDriver(ctx context.Context, logger *slog.Logger, dsn *url.URL) *entsql.Driver {
	driver, err := Driver(ctx, logger, dsn)
	if err != nil {
		panic(err)
	}
	return driver
}

// Driver returns a new database driver for the given DSN. If logger is nil,
// [slog.Default] is used. Supports the following schemes:
// - sqlite3 (see [DefaultSQLitePragmas] for default pragmas, optimizing for single-process performance))
// - sqlite (remapped to sqlite3)
// - file (remapped to sqlite)
// - memory through "sqlite::memory:", each call to [MemoryDriver] will create a unique in-memory database.
// - postgres (using pgx, with pgxpool support, see [pgxpool.ParseConfig] for supported pooling options).
//
// Example:
//
//	driver, err := entx.Driver(ctx, logger, dsn)
//	if err != nil {
//		panic(err)
//	}
//	ent.NewClient(ent.Driver(driver))
func Driver(ctx context.Context, logger *slog.Logger, dsn *url.URL) (*entsql.Driver, error) {
	if dsn == nil {
		return nil, errors.New("dsn is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

check:

	switch {
	case dsn.Scheme == "file":
		dsn.Scheme = "sqlite3"
		goto check
	case dsn.Scheme == "sqlite":
		dsn.Scheme = "sqlite3"
		goto check
	case dsn.Opaque == "memory" || dsn.Opaque == ":memory:":
		params := sqlitePragmaValues(DefaultSQLitePragmas...)
		params.Set("mode", "memory")
		params.Set("cache", "shared")
		dsn = &url.URL{
			Scheme:   "sqlite3",
			Opaque:   "ent-" + uuid.NewV7().String(),
			RawQuery: params.Encode(),
		}
		goto check
	case dsn.Scheme == "sqlite3" && !dsn.Query().Has("_pragma"):
		params := dsn.Query()
		for _, p := range DefaultSQLitePragmas {
			params.Add("_pragma", p[0]+"("+p[1]+")")
		}
		params.Set("_txlock", "immediate")
		dsn.RawQuery = params.Encode()
	}

	var db *sql.DB

	logger.InfoContext(ctx, "opening database", "dsn", dsn.Redacted())

	switch dsn.Scheme {
	case dialect.SQLite:
		sqliteInit()

		var err error
		db, err = sql.Open(dsn.Scheme, dsn.String())
		if err != nil {
			return nil, fmt.Errorf("opening %s: %w", dsn.Scheme, err)
		}
		db.SetMaxOpenConns(1)

		return entsql.OpenDB(dialect.SQLite, db), nil
	case dialect.Postgres:
		poolConfig, err := pgxpool.ParseConfig(dsn.String())
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", dsn.Scheme, err)
		}

		var pdb *pgxpool.Pool

		databaseLogger := func() *slog.Logger {
			stats := pdb.Stat()
			return logger.With(
				slog.Group(
					"database",
					"active", stats.ConstructingConns()+stats.AcquiredConns(),
					"idle", stats.IdleConns(),
					"max", stats.MaxConns(),
				),
			)
		}

		poolConfig.MaxConnIdleTime = 5 * time.Minute
		poolConfig.PingTimeout = 5 * time.Second
		poolConfig.BeforeConnect = func(ctx context.Context, _ *pgx.ConnConfig) error {
			databaseLogger().DebugContext(ctx, "initializing new connection")
			return nil
		}
		poolConfig.AfterConnect = func(ctx context.Context, _ *pgx.Conn) error {
			databaseLogger().DebugContext(ctx, "connection initialized")
			return nil
		}
		poolConfig.BeforeClose = func(_ *pgx.Conn) {
			databaseLogger().DebugContext(ctx, "closing connection")
		}

		pdb, err = pgxpool.NewWithConfig(ctx, poolConfig)
		if err != nil {
			return nil, fmt.Errorf("creating %s: %w", dsn.Scheme, err)
		}

		return entsql.OpenDB(dialect.Postgres, stdlib.OpenDBFromPool(pdb)), nil
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", dsn.Scheme)
	}
}
