package leader

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeState holds the shared state for fake Redis operations.
type fakeState struct {
	mu   sync.Mutex
	lock string
}

func (s *fakeState) fakeSetNX(_ context.Context, _ string, value string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock != "" {
		return false, nil
	}
	s.lock = value
	return true, nil
}

func (s *fakeState) fakeGet(_ context.Context, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lock, nil
}

func (s *fakeState) fakeSet(_ context.Context, _ string, value string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lock = value
	return true, nil
}

func (s *fakeState) fakeDel(_ context.Context, keys ...string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock != "" {
		s.lock = ""
		return 1, nil
	}
	return 0, nil
}

func (s *fakeState) fakeDelIfMatch(_ context.Context, key, expected string) (bool, error) {
	_ = key
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == expected {
		s.lock = ""
		return true, nil
	}
	return false, nil
}

// newFakeOps creates a fresh Ops backed by a new fakeState.
func newFakeOps() Ops {
	fs := &fakeState{}
	return Ops{
		SetNX:      fs.fakeSetNX,
		Get:        fs.fakeGet,
		Set:        fs.fakeSet,
		Del:        fs.fakeDel,
		DelIfMatch: fs.fakeDelIfMatch,
	}
}

func TestAcquireLock_Success(t *testing.T) {
	ops := newFakeOps()
	e, err := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	if err != nil {
		t.Fatalf("NewLeaderElection: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	acquired, err := e.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !acquired {
		t.Error("expected lock acquisition to succeed")
	}
}

func TestAcquireLock_AlreadyHeld(t *testing.T) {
	// Create a fake with a pre-existing lock.
	fs := &fakeState{lock: "other-instance"}
	ops := Ops{
		SetNX:      fs.fakeSetNX,
		Get:        fs.fakeGet,
		Set:        fs.fakeSet,
		Del:        fs.fakeDel,
		DelIfMatch: fs.fakeDelIfMatch,
	}
	e, _ := NewLeaderElection(ops, "test-lock", "instance-2", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	acquired, err := e.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if acquired {
		t.Error("expected lock acquisition to fail (already held)")
	}
}

func TestRenewLock_Success(t *testing.T) {
	ops := newFakeOps()
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = e.Acquire(ctx)

	renewed, err := e.Renew(ctx)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !renewed {
		t.Error("expected renew to succeed")
	}
}

func TestRenewLock_FailsWhenNotLeader(t *testing.T) {
	fs := &fakeState{lock: "other-instance"}
	ops := Ops{
		SetNX:      fs.fakeSetNX,
		Get:        fs.fakeGet,
		Set:        fs.fakeSet,
		Del:        fs.fakeDel,
		DelIfMatch: fs.fakeDelIfMatch,
	}
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	renewed, err := e.Renew(ctx)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if renewed {
		t.Error("expected renew to fail (not leader)")
	}
}

func TestReleaseLock_Success(t *testing.T) {
	ops := newFakeOps()
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = e.Acquire(ctx)

	released, err := e.Release(ctx)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if !released {
		t.Error("expected release to succeed")
	}
}

func TestReleaseLock_FailsWhenNotLeader(t *testing.T) {
	fs := &fakeState{lock: "other-instance"}
	ops := Ops{
		SetNX:      fs.fakeSetNX,
		Get:        fs.fakeGet,
		Set:        fs.fakeSet,
		Del:        fs.fakeDel,
		DelIfMatch: fs.fakeDelIfMatch,
	}
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	released, err := e.Release(ctx)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if released {
		t.Error("expected release to fail (not leader)")
	}
}

func TestIsLeader(t *testing.T) {
	ops := newFakeOps()
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if e.IsLeader() {
		t.Error("expected IsLeader() to return false before acquisition")
	}

	_, _ = e.Acquire(ctx)
	if !e.IsLeader() {
		t.Error("expected IsLeader() to return true after acquisition")
	}

	e.Release(ctx)
	if e.IsLeader() {
		t.Error("expected IsLeader() to return false after release")
	}
}

func TestLeaderID(t *testing.T) {
	ops := newFakeOps()
	e, _ := NewLeaderElection(ops, "test-lock", "my-instance-42", 15*time.Second)
	// Before acquiring, no lock is held so LeaderID returns empty.
	if e.LeaderID() != "" {
		t.Errorf("LeaderID (no lock): got %q, want ''", e.LeaderID())
	}

	_, _ = e.Acquire(context.Background())
	// After acquiring, LeaderID returns our instance ID.
	if e.LeaderID() != "my-instance-42" {
		t.Errorf("LeaderID (after acquire): got %q, want 'my-instance-42'", e.LeaderID())
	}
}

func TestLeaderID_ReturnsEmptyWhenNoLeader(t *testing.T) {
	ops := newFakeOps()
	e, _ := NewLeaderElection(ops, "test-lock", "my-instance", 15*time.Second)
	if e.LeaderID() != "" {
		t.Errorf("LeaderID: got %q, want empty string (no leader)", e.LeaderID())
	}
}

func TestLeaderID_ReturnsHolderWhenOtherLeads(t *testing.T) {
	fs := &fakeState{lock: "rival"}
	ops := Ops{
		SetNX:      fs.fakeSetNX,
		Get:        fs.fakeGet,
		Set:        fs.fakeSet,
		Del:        fs.fakeDel,
		DelIfMatch: fs.fakeDelIfMatch,
	}
	e, _ := NewLeaderElection(ops, "test-lock", "my-instance", 15*time.Second)
	if e.LeaderID() != "rival" {
		t.Errorf("LeaderID: got %q, want 'rival'", e.LeaderID())
	}
}

func TestLockTTL(t *testing.T) {
	ops := newFakeOps()
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 30*time.Second)
	if e.LockTTL() != 30*time.Second {
		t.Errorf("LockTTL: got %v, want 30s", e.LockTTL())
	}
}

func TestDefaultLockTTL(t *testing.T) {
	ops := newFakeOps()
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 0)
	if e.LockTTL() != LockTTLDefault {
		t.Errorf("LockTTL: got %v, want %v (default)", e.LockTTL(), LockTTLDefault)
	}
}

func TestRenewLoop_ContextCancelled(t *testing.T) {
	ops := newFakeOps()
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _ = e.Acquire(ctx)

	errCh := make(chan error, 1)
	go func() {
		errCh <- e.RenewLoop(ctx, 500*time.Millisecond)
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("RenewLoop should return error when context is cancelled")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RenewLoop did not exit after context canceled")
	}
}

func TestRenewLoop_StopsWhenLockLost(t *testing.T) {
	fs := &fakeState{}
	ops := Ops{
		SetNX:      fs.fakeSetNX,
		Get:        fs.fakeGet,
		Set:        fs.fakeSet,
		Del:        fs.fakeDel,
		DelIfMatch: fs.fakeDelIfMatch,
	}
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, _ = e.Acquire(ctx)

	errCh := make(chan error, 1)
	go func() {
		errCh <- e.RenewLoop(ctx, 300*time.Millisecond)
	}()

	time.Sleep(500 * time.Millisecond)
	fs.lock = "instance-2" // steal the lock

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("RenewLoop should return error when lock is lost")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RenewLoop did not exit after losing lock")
	}
}

func TestRenewLoop_NeverStartsWhenNotLeader(t *testing.T) {
	fs := &fakeState{lock: "other-instance"}
	ops := Ops{
		SetNX:      fs.fakeSetNX,
		Get:        fs.fakeGet,
		Set:        fs.fakeSet,
		Del:        fs.fakeDel,
		DelIfMatch: fs.fakeDelIfMatch,
	}
	e, _ := NewLeaderElection(ops, "test-lock", "our-instance", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- e.RenewLoop(ctx, 200*time.Millisecond)
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("RenewLoop should return error when not leader")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RenewLoop did not exit when not leader")
	}
}

func TestConcurrentAcquire(t *testing.T) {
	ops := newFakeOps()
	e1, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	e2, _ := NewLeaderElection(ops, "test-lock", "instance-2", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	acq1, _ := e1.Acquire(ctx)
	if !acq1 {
		t.Fatal("instance-1 should acquire lock")
	}

	acq2, _ := e2.Acquire(ctx)
	if acq2 {
		t.Error("instance-2 should NOT acquire lock (already held)")
	}

	if !e1.IsLeader() {
		t.Error("instance-1 should be leader")
	}
	if e2.IsLeader() {
		t.Error("instance-2 should NOT be leader")
	}

	e1.Release(ctx)

	acq2, _ = e2.Acquire(ctx)
	if !acq2 {
		t.Error("instance-2 should acquire lock after instance-1 releases")
	}
}

func TestFullLifecycle(t *testing.T) {
	ops := newFakeOps()
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Acquire
	acquired, _ := e.Acquire(ctx)
	if !acquired {
		t.Fatal("should acquire lock")
	}
	if !e.IsLeader() {
		t.Fatal("should be leader")
	}

	// Renew
	for i := 0; i < 3; i++ {
		renewed, err := e.Renew(ctx)
		if err != nil {
			t.Fatalf("Renew %d: %v", i, err)
		}
		if !renewed {
			t.Fatalf("Renew %d should succeed", i)
		}
		if !e.IsLeader() {
			t.Fatalf("Should still be leader after renew %d", i)
		}
	}

	// Release
	released, _ := e.Release(ctx)
	if !released {
		t.Fatal("should release lock")
	}
	if e.IsLeader() {
		t.Fatal("should not be leader after release")
	}
}

func TestRenew_WhenLockStolen(t *testing.T) {
	fs := &fakeState{}
	ops := Ops{
		SetNX:      fs.fakeSetNX,
		Get:        fs.fakeGet,
		Set:        fs.fakeSet,
		Del:        fs.fakeDel,
		DelIfMatch: fs.fakeDelIfMatch,
	}
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = e.Acquire(ctx)
	fs.lock = "instance-2" // simulate lock stolen

	renewed, err := e.Renew(ctx)
	if err != nil {
		t.Fatalf("Renew: unexpected error: %v", err)
	}
	if renewed {
		t.Fatal("Renew should fail when lock was stolen")
	}
}

func TestRelease_WhenLockStolen(t *testing.T) {
	fs := &fakeState{}
	ops := Ops{
		SetNX:      fs.fakeSetNX,
		Get:        fs.fakeGet,
		Set:        fs.fakeSet,
		Del:        fs.fakeDel,
		DelIfMatch: fs.fakeDelIfMatch,
	}
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = e.Acquire(ctx)
	fs.lock = "instance-2" // simulate lock stolen

	released, err := e.Release(ctx)
	if err != nil {
		t.Fatalf("Release: unexpected error: %v", err)
	}
	if released {
		t.Fatal("Release should fail when lock was stolen")
	}
}

func TestAcquireIdempotent(t *testing.T) {
	// Acquire uses SET NX which is not idempotent.
	// After first acquire, second Acquire returns false (key exists).
	// But IsLeader() correctly returns true.
	ops := newFakeOps()
	e, _ := NewLeaderElection(ops, "test-lock", "instance-1", 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	acq1, _ := e.Acquire(ctx)
	if !acq1 {
		t.Fatal("first acquire should succeed")
	}
	if !e.IsLeader() {
		t.Fatal("should be leader after first acquire")
	}
	// Second acquire returns false (SET NX fails, key exists).
	// But we're still the leader.
	acq2, _ := e.Acquire(ctx)
	if acq2 {
		t.Error("second acquire should return false (SET NX not idempotent)")
	}
	if !e.IsLeader() {
		t.Fatal("should still be leader")
	}
}
