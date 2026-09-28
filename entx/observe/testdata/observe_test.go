// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build observe_gen

package testdata_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"strings"
	"sync"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	moderncsqlite "modernc.org/sqlite"

	"github.com/lrstanley/x/entx/observe/testdata/ent"
	"github.com/lrstanley/x/entx/observe/testdata/ent/user"
)

var registerSQLite = sync.OnceFunc(func() {
	sql.Register(dialect.SQLite, &moderncsqlite.Driver{})
})

func openClient(t *testing.T) *ent.Client {
	t.Helper()
	registerSQLite()
	dsn := "file:ent_" + t.Name() + "?mode=memory&cache=shared&_fk=1"
	db, err := entsql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	client := ent.NewClient(ent.Driver(db))
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return client
}

func TestObserve_createUpdateDelete(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()

	var events []ent.ObserverEvent
	client.Use(ent.ObserveUserFields(func(_ context.Context, ev ent.ObserverEvent) error {
		events = append(events, ev)
		return nil
	}))

	u1, err := client.User.Create().SetName("a").Save(ctx)
	if err != nil {
		t.Fatalf("create u1: %v", err)
	}
	u2, err := client.User.Create().SetName("b").Save(ctx)
	if err != nil {
		t.Fatalf("create u2: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("after create: got %d events, want 2", len(events))
	}
	if events[0].Op != ent.OpCreate || events[0].ID != u1.ID {
		t.Fatalf("create event[0] = op=%v id=%v, want create/%d", events[0].Op, events[0].ID, u1.ID)
	}
	if events[0].Fields["name"].Value != "a" {
		t.Fatalf("create fields = %#v", events[0].Fields)
	}

	events = nil
	n, err := client.User.Update().Where(user.IDIn(u1.ID, u2.ID)).SetName("z").Save(ctx)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if n != 2 {
		t.Fatalf("updated %d, want 2", n)
	}
	if len(events) != 2 {
		t.Fatalf("after update: got %d events, want 2", len(events))
	}
	for _, ev := range events {
		if ev.Op != ent.OpUpdate {
			t.Fatalf("update op = %v", ev.Op)
		}
		if ev.Fields["name"].Value != "z" {
			t.Fatalf("update fields = %#v", ev.Fields)
		}
		if ev.Entity != nil {
			t.Fatalf("fields mode should not set entity")
		}
	}

	events = nil
	n, err = client.User.Delete().Where(user.IDIn(u1.ID, u2.ID)).Exec(ctx)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted %d, want 2", n)
	}
	if len(events) != 2 {
		t.Fatalf("after delete: got %d events, want 2", len(events))
	}
	for _, ev := range events {
		if ev.Op != ent.OpDelete {
			t.Fatalf("delete op = %v", ev.Op)
		}
		if ev.Fields != nil {
			t.Fatalf("delete should have no fields, got %#v", ev.Fields)
		}
	}
}

func TestObserve_fullObjectAndUUID(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()

	var events []ent.ObserverEvent
	client.Use(ent.Observe(func(_ context.Context, ev ent.ObserverEvent) error {
		events = append(events, ev)
		return nil
	}))

	doc, err := client.Document.Create().SetTitle("t1").Save(ctx)
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != ent.TypeDocument {
		t.Fatalf("type = %q", events[0].Type)
	}
	id, ok := events[0].ID.(uuid.UUID)
	if !ok || id != doc.ID {
		t.Fatalf("id = %v (%T), want %v", events[0].ID, events[0].ID, doc.ID)
	}
	entity, ok := events[0].Entity.(*ent.Document)
	if !ok || entity == nil || entity.Title != "t1" {
		t.Fatalf("entity = %#v", events[0].Entity)
	}

	events = nil
	_, err = client.Document.UpdateOneID(doc.ID).SetTitle("t2").Save(ctx)
	if err != nil {
		t.Fatalf("update document: %v", err)
	}
	if len(events) != 1 || events[0].Op != ent.OpUpdateOne {
		t.Fatalf("update events = %#v", events)
	}
	entity, ok = events[0].Entity.(*ent.Document)
	if !ok || entity.Title != "t2" {
		t.Fatalf("updated entity = %#v", events[0].Entity)
	}

	events = nil
	err = client.Document.DeleteOneID(doc.ID).Exec(ctx)
	if err != nil {
		t.Fatalf("delete document: %v", err)
	}
	if len(events) != 1 || events[0].Op != ent.OpDeleteOne {
		t.Fatalf("delete events = %#v", events)
	}
	entity, ok = events[0].Entity.(*ent.Document)
	if !ok || entity.Title != "t2" {
		t.Fatalf("deleted entity = %#v", events[0].Entity)
	}
}

func TestObserve_variadicFilter(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()

	var events []ent.ObserverEvent
	client.Use(ent.ObserveID(func(_ context.Context, ev ent.ObserverEvent) error {
		events = append(events, ev)
		return nil
	}, ent.TypeUser))

	_, err := client.User.Create().SetName("u").Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	_, err = client.Document.Create().SetTitle("d").Save(ctx)
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	if len(events) != 1 || events[0].Type != ent.TypeUser {
		t.Fatalf("events = %#v, want single User", events)
	}
}

func TestObserve_handlerError(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()
	want := errors.New("boom")

	client.Use(ent.ObserveUserID(func(context.Context, ent.ObserverEvent) error {
		return want
	}))

	_, err := client.User.Create().SetName("x").Save(ctx)
	if !errors.Is(err, want) {
		t.Fatalf("Save error = %v, want %v", err, want)
	}
}

func TestObserve_txCommitDelivers(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()

	var events []ent.ObserverEvent
	client.Use(ent.ObserveUserID(func(_ context.Context, ev ent.ObserverEvent) error {
		events = append(events, ev)
		return nil
	}))

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	txctx := ent.NewTxContext(ctx, tx)

	u, err := tx.User.Create().SetName("tx").Save(txctx)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events before commit: %#v", events)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("after commit: got %d events, want 1", len(events))
	}
	if events[0].Op != ent.OpCreate || events[0].ID != u.ID {
		t.Fatalf("event = %#v, want create/%d", events[0], u.ID)
	}
}

func TestObserve_txMultiMutateOrder(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()

	var events []ent.ObserverEvent
	client.Use(ent.ObserveUserID(func(_ context.Context, ev ent.ObserverEvent) error {
		events = append(events, ev)
		return nil
	}))

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	txctx := ent.NewTxContext(ctx, tx)

	u1, err := tx.User.Create().SetName("first").Save(txctx)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("create u1: %v", err)
	}
	u2, err := tx.User.Create().SetName("second").Save(txctx)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("create u2: %v", err)
	}
	if _, err := tx.User.UpdateOneID(u1.ID).SetName("updated").Save(txctx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("update u1: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events before commit: %#v", events)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("after commit: got %d events, want 3", len(events))
	}
	if events[0].Op != ent.OpCreate || events[0].ID != u1.ID {
		t.Fatalf("event[0] = %#v, want create/%d", events[0], u1.ID)
	}
	if events[1].Op != ent.OpCreate || events[1].ID != u2.ID {
		t.Fatalf("event[1] = %#v, want create/%d", events[1], u2.ID)
	}
	if events[2].Op != ent.OpUpdateOne || events[2].ID != u1.ID {
		t.Fatalf("event[2] = %#v, want updateOne/%d", events[2], u1.ID)
	}

	events = nil
	tx, err = client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin rollback tx: %v", err)
	}
	txctx = ent.NewTxContext(ctx, tx)
	if _, err := tx.User.Create().SetName("rb1").Save(txctx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("create rb1: %v", err)
	}
	if _, err := tx.User.Create().SetName("rb2").Save(txctx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("create rb2: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events after multi-mutate rollback: %#v", events)
	}
}

func TestObserve_txRollbackDrops(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()

	var events []ent.ObserverEvent
	client.Use(ent.ObserveUserID(func(_ context.Context, ev ent.ObserverEvent) error {
		events = append(events, ev)
		return nil
	}))

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	txctx := ent.NewTxContext(ctx, tx)

	_, err = tx.User.Create().SetName("rb").Save(txctx)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("create: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events before rollback: %#v", events)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events after rollback: %#v", events)
	}

	n, err := client.User.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows after rollback = %d, want 0", n)
	}
}

func TestObserve_txNoCommitDrops(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()

	var events []ent.ObserverEvent
	client.Use(ent.ObserveUserID(func(_ context.Context, ev ent.ObserverEvent) error {
		events = append(events, ev)
		return nil
	}))

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.User.Create().SetName("nc").Save(ent.NewTxContext(ctx, tx))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events without commit: %#v", events)
	}
}

func TestObserve_txHandlerErrorAfterCommit(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()

	client.Use(ent.ObserveUserID(func(context.Context, ent.ObserverEvent) error {
		return errors.New("boom")
	}))

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	txctx := ent.NewTxContext(ctx, tx)

	u, err := tx.User.Create().SetName("ok").Save(txctx)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("create: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	got, err := client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatalf("get after commit: %v", err)
	}
	if got.Name != "ok" {
		t.Fatalf("name = %q, want ok", got.Name)
	}
}

func TestObserve_eventJSON(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()

	var got ent.ObserverEvent
	client.Use(ent.ObserveUserID(func(_ context.Context, ev ent.ObserverEvent) error {
		got = ev
		return nil
	}))

	u, err := client.User.Create().SetName("j").Save(ctx)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("expected non-zero id from sqlite")
	}

	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, "mutation") || strings.Contains(s, `"Mutation"`) {
		t.Fatalf("json includes mutation: %s", s)
	}
	if strings.Contains(s, `"entity"`) {
		t.Fatalf("json includes entity: %s", s)
	}
	if !strings.Contains(s, `"op":"create"`) {
		t.Fatalf("json missing op: %s", s)
	}

	zero := ent.ObserverEvent{Op: ent.OpCreate, Type: ent.TypeUser, ID: 0}
	b, err = json.Marshal(zero)
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if !strings.Contains(string(b), `"id":0`) {
		t.Fatalf("zero id omitted: %s", b)
	}
	if strings.Contains(string(b), `"entity"`) {
		t.Fatalf("empty entity present: %s", b)
	}
}

func TestObserve_unknownTypePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "unknown type") {
			t.Fatalf("panic = %v", r)
		}
	}()
	_ = ent.ObserveID(func(context.Context, ent.ObserverEvent) error { return nil }, "Nope")
}

func TestObserve_edgeEagerLoadOptOut(t *testing.T) {
	client := openClient(t)
	ctx := context.Background()

	u, err := client.User.Create().SetName("owner").Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	doc, err := client.Document.Create().SetTitle("doc").SetAuthor(u).Save(ctx)
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	note, err := client.Note.Create().SetText("n1").SetOwner(u).Save(ctx)
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	var events []ent.ObserverEvent
	client.Use(ent.ObserveUser(func(_ context.Context, ev ent.ObserverEvent) error {
		events = append(events, ev)
		return nil
	}))

	_, err = client.User.UpdateOneID(u.ID).SetName("owner2").Save(ctx)
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	entity, ok := events[0].Entity.(*ent.User)
	if !ok || entity == nil {
		t.Fatalf("entity = %#v", events[0].Entity)
	}

	docs, err := entity.Edges.DocumentsOrErr()
	if err != nil {
		t.Fatalf("documents edge not loaded: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != doc.ID {
		t.Fatalf("documents = %#v, want [%v]", docs, doc.ID)
	}

	if _, err := entity.Edges.NotesOrErr(); err == nil {
		t.Fatal("notes edge should not be loaded (WithEagerLoad(false) opt-out)")
	}

	// Full-object Document observation does not load author (no edge annotations).
	events = nil
	client.Use(ent.ObserveDocument(func(_ context.Context, ev ent.ObserverEvent) error {
		events = append(events, ev)
		return nil
	}))
	_, err = client.Document.UpdateOneID(doc.ID).SetTitle("doc2").Save(ctx)
	if err != nil {
		t.Fatalf("update document: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d document events, want 1", len(events))
	}
	dent, ok := events[0].Entity.(*ent.Document)
	if !ok || dent == nil {
		t.Fatalf("document entity = %#v", events[0].Entity)
	}
	if _, err := dent.Edges.AuthorOrErr(); err == nil {
		t.Fatal("author edge should not be loaded without annotations")
	}

	// Sanity: note still exists (opt-out is about eager-load, not deletion).
	if _, err := client.Note.Get(ctx, note.ID); err != nil {
		t.Fatalf("get note: %v", err)
	}
}
