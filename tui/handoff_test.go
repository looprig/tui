package tui

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestReopenHandoffFinalizerWaitsForBlockedOpen models forced Program.Run termination while
// the opener is still in flight. Finalization cannot return until the command completes, and
// it closes the successful but unconsumed replacement exactly once.
func TestReopenHandoffFinalizerWaitsForBlockedOpen(t *testing.T) {
	old := &fakeAgent{}
	fresh := &fakeAgent{}
	entered := make(chan struct{})
	release := make(chan struct{})
	handoff := newReopenHandoff()
	cmd := reopenAgent(context.Background(), old, func(context.Context) (Agent, error) {
		close(entered)
		<-release
		return fresh, nil
	}, handoff)
	go func() { _ = cmd() }()
	<-entered

	done := make(chan error, 1)
	go func() { done <- handoff.finalize() }()
	assertStillBlocked(t, done)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if old.closeCalls != 1 || fresh.closeCalls != 1 {
		t.Fatalf("Close calls old/fresh = %d/%d, want 1/1", old.closeCalls, fresh.closeCalls)
	}
}

// TestReopenHandoffFinalizerWaitsForBlockedClose covers the earlier ownership phase: the
// runtime barrier also waits while old-session shutdown is in flight before the opener runs.
func TestReopenHandoffFinalizerWaitsForBlockedClose(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	old := &fakeAgent{closeEntered: entered, closeRelease: release}
	fresh := &fakeAgent{}
	var openerCalled bool
	handoff := newReopenHandoff()
	cmd := reopenAgent(context.Background(), old, func(context.Context) (Agent, error) {
		openerCalled = true
		return fresh, nil
	}, handoff)
	go func() { _ = cmd() }()
	<-entered

	done := make(chan error, 1)
	go func() { done <- handoff.finalize() }()
	assertStillBlocked(t, done)
	if openerCalled {
		t.Fatal("opener ran before blocked old Close completed")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if old.closeCalls != 1 || fresh.closeCalls != 1 {
		t.Fatalf("Close calls old/fresh = %d/%d, want 1/1", old.closeCalls, fresh.closeCalls)
	}
}

// TestReopenHandoffCancellationWaitsForCommandExit proves signal cancellation reaches the
// opener and finalization observes command completion before returning its terminal error.
func TestReopenHandoffCancellationWaitsForCommandExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	old := &fakeAgent{}
	entered := make(chan struct{})
	handoff := newReopenHandoff()
	cmd := reopenAgent(ctx, old, func(ctx context.Context) (Agent, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}, handoff)
	go func() { _ = cmd() }()
	<-entered
	cancel()
	if err := handoff.finalize(); !errors.Is(err, context.Canceled) {
		t.Fatalf("finalize error = %v, want context.Canceled", err)
	}
	if old.closeCalls != 1 {
		t.Fatalf("old Close calls = %d, want 1", old.closeCalls)
	}
}

func assertStillBlocked(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("finalize returned early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}
