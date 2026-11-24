package poller_test

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunReturnsOnlyAfterTheRunningRefreshFinished(t *testing.T) {
	r := newRig(t)
	gate, entered := make(chan struct{}), make(chan struct{}, 1)
	r.gh.set(func(f *fakeGitHub) { f.gate, f.entered = gate, entered })

	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() {
		r.p.Run(ctx)
		close(returned)
	}()
	<-entered
	cancel()
	select {
	case <-returned:
		t.Fatal("Run returned while a refresh was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the refresh finished")
	}
	if r.p.Status().Running {
		t.Fatal("a refresh outlived Run")
	}
}

func TestNoRefreshStartsAfterRunReturned(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.p.Run(ctx)
		close(done)
	}()
	r.p.Trigger()
	cancel()
	<-done
	before, _ := r.gh.calls()
	if err := r.p.Sync(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sync after Run: %v", err)
	}
	r.p.Trigger()
	if after, _ := r.gh.calls(); after != before {
		t.Fatalf("a refresh started after shutdown: %d calls became %d", before, after)
	}
}

func TestSyncBeforeRunStillRefreshes(t *testing.T) {
	r := newRig(t)
	if err := r.p.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if open, _ := r.gh.calls(); open != 1 {
		t.Fatalf("calls %d", open)
	}
	if r.snapshot().Version != 1 {
		t.Fatalf("version %d", r.snapshot().Version)
	}
}
