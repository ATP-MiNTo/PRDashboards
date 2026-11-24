package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

func TestRecordMergeWritesEverythingInOneTransaction(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	if _, err := rawDB(t, path).Exec(`INSERT INTO sync_rules (id, position, name, trigger, target_status) VALUES ('r1', 0, 'n', '{"type":"merged"}', 'Done')`); err != nil {
		t.Fatal(err)
	}
	err := s.RecordMerge(ctx, store.Merged{
		Now: clock.Now(), PRID: "P1", Key: "k1",
		Activity: []store.ActivityRow{{Kind: "merge", PRID: "P1", TriggeredBy: "manual", Result: "ok", Summary: "Merged"}, {Kind: "transition", TicketKey: "INP-1", PRID: "P1", TriggeredBy: "manual", Result: "failed", Summary: "x", Error: "boom"}},
		Retries:  []store.RetryRow{{PRID: "P1", TicketKey: "INP-1", TransitionID: "31", ToStatus: "Done", LastError: "boom"}},
		Firings:  []store.Firing{{Rule: "r1", Ticket: "INP-1", Fingerprint: "P1=P1"}, {Rule: "r1", Ticket: "INP-2", Fingerprint: "P1=P1"}, {Rule: "gone", Ticket: "INP-3", Fingerprint: "x"}},
		Encode: func(ids map[string]int64) ([]byte, error) {
			if ids["INP-1"] == 0 {
				t.Error("retry id missing")
			}
			return []byte(`{"ok":true}`), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, path, `SELECT count(*) FROM activity`); n != 2 {
		t.Fatalf("activity %d", n)
	}
	if n := count(t, path, `SELECT count(*) FROM pending_retries WHERE fields = '{}'`); n != 1 {
		t.Fatalf("retries %d", n)
	}
	if n := count(t, path, `SELECT count(*) FROM rule_firings`); n != 2 {
		t.Fatalf("firings %d", n)
	}
	pr, raw, ok, err := s.Idempotent(ctx, "k1", clock.Now())
	if err != nil || !ok || pr != "P1" || string(raw) != `{"ok":true}` {
		t.Fatalf("%q %s %v %v", pr, raw, ok, err)
	}
	if _, _, ok, _ := s.Idempotent(ctx, "other", clock.Now()); ok {
		t.Fatal("unknown key found")
	}
}

func TestRecordMergeFailureWritesNothing(t *testing.T) {
	s, path, clock := open(t)
	err := s.RecordMerge(context.Background(), store.Merged{
		Now: clock.Now(), PRID: "P1", Key: "k1",
		Activity: []store.ActivityRow{{Kind: "merge", Result: "ok", TriggeredBy: "manual"}},
		Retries:  []store.RetryRow{{TicketKey: "INP-1", TransitionID: "31"}},
		Encode:   func(map[string]int64) ([]byte, error) { return nil, errors.New("no") },
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, table := range []string{"activity", "pending_retries", "idempotency"} {
		if n := count(t, path, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%s has %d rows after a rolled back merge", table, n)
		}
	}
}

func TestIdempotencyExpiresAndIsPurged(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	rec := func(key string) error {
		return s.RecordMerge(ctx, store.Merged{Now: clock.Now(), PRID: "P1", Key: key, Encode: func(map[string]int64) ([]byte, error) { return []byte("{}"), nil }})
	}
	if err := rec("old"); err != nil {
		t.Fatal(err)
	}
	clock.Advance(store.IdempotencyTTL + time.Second)
	if _, _, ok, _ := s.Idempotent(ctx, "old", clock.Now()); ok {
		t.Fatal("an expired key is still honoured")
	}
	if err := rec("new"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, path, `SELECT count(*) FROM idempotency`); n != 1 {
		t.Fatalf("expired rows are purged on the next merge: %d", n)
	}
	if err := rec("new"); err != nil {
		t.Fatalf("rewriting a key: %v", err)
	}
}

func TestRetriesLifecycle(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	if err := s.RecordMerge(ctx, store.Merged{
		Now: clock.Now(), PRID: "P1", Key: "k",
		Retries: []store.RetryRow{
			{PRID: "P1", TicketKey: "INP-1", TransitionID: "31", ToStatus: "Done", Fields: map[string]string{"resolution": "1"}, LastError: "a"},
			{TicketKey: "INP-2", TransitionID: "21", LastError: "b"},
		},
		Encode: func(map[string]int64) ([]byte, error) { return []byte("{}"), nil },
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Retries(ctx)
	if err != nil || len(rows) != 2 || rows[0].Fields["resolution"] != "1" || rows[1].PRID != "" {
		t.Fatalf("%+v %v", rows, err)
	}
	one, err := s.Retry(ctx, rows[0].ID)
	if err != nil || one.TicketKey != "INP-1" {
		t.Fatalf("%+v %v", one, err)
	}
	if _, err := s.Retry(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("%v", err)
	}

	fail := store.ActivityRow{Kind: "transition", TicketKey: "INP-1", TriggeredBy: "retry", Result: "failed", Summary: "s", Error: "again"}
	if err := s.FinishRetry(ctx, rows[0].ID, clock.Now(), fail); err != nil {
		t.Fatal(err)
	}
	if one, _ = s.Retry(ctx, rows[0].ID); one.LastError != "again" {
		t.Fatalf("%+v", one)
	}
	ok := fail
	ok.Result, ok.Error = "ok", ""
	if err := s.FinishRetry(ctx, rows[0].ID, clock.Now(), ok); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRetry(ctx, rows[0].ID, clock.Now(), ok); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if err := s.DeleteRetry(ctx, rows[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRetry(ctx, rows[1].ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if rows, _ := s.Retries(ctx); len(rows) != 0 {
		t.Fatalf("%+v", rows)
	}
}

func TestActivityPagesNewestFirst(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	for i := range 5 {
		if err := s.RecordMerge(ctx, store.Merged{
			Now: clock.Now(), PRID: "P", Key: string(rune('a' + i)),
			Activity: []store.ActivityRow{{Kind: "merge", PRID: "P", TriggeredBy: "manual", Result: "ok", Summary: string(rune('a' + i))}},
			Encode:   func(map[string]int64) ([]byte, error) { return []byte("{}"), nil },
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.Activity(ctx, 0, 2)
	if err != nil || len(page) != 2 || page[0].Summary != "e" || page[1].Summary != "d" || page[0].TicketKey != "" {
		t.Fatalf("%+v %v", page, err)
	}
	rest, _ := s.Activity(ctx, page[1].ID, 10)
	if len(rest) != 3 || rest[0].Summary != "c" || rest[2].Summary != "a" {
		t.Fatalf("%+v", rest)
	}
}
