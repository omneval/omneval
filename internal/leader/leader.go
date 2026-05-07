// Package leader provides Redis-based leader election for the Writer Service.
// Uses the standard Redis SET NX PX lock pattern for active-passive HA.
package leader

import (
	"context"
	"fmt"
	"time"
)

// LockTTLDefault is the default leader election lock TTL (15 seconds).
const LockTTLDefault = 15 * time.Second

// RenewIntervalDefault is the default renewal interval (5 seconds).
const RenewIntervalDefault = 5 * time.Second

// Ops holds the Redis operations used by leader election, allowing a fake
// implementation to be injected in tests.
type Ops struct {
	SetNX      func(ctx context.Context, key, value string, expiration time.Duration) (bool, error)
	Get        func(ctx context.Context, key string) (string, error)
	Set        func(ctx context.Context, key, value string, expiration time.Duration) (bool, error)
	Del        func(ctx context.Context, keys ...string) (int64, error)
	DelIfMatch func(ctx context.Context, key, expected string) (bool, error)
}

// LeaderElection implements Redis-based leader election using the SET NX PX
// lock pattern (SET key value NX PX ttl).
type LeaderElection struct {
	ops      Ops
	lockKey  string
	instance string
	ttl      time.Duration
}

// NewLeaderElection creates a new LeaderElection. Pass ops from
// NewOpsFromRedis for production use. The ttl is clamped to LockTTLDefault
// when zero or negative.
func NewLeaderElection(ops Ops, lockKey, instance string, ttl time.Duration) (*LeaderElection, error) {
	if ttl <= 0 {
		ttl = LockTTLDefault
	}
	return &LeaderElection{
		ops:      ops,
		lockKey:  lockKey,
		instance: instance,
		ttl:      ttl,
	}, nil
}

// Acquire tries to acquire the leader lock via SET NX. Returns (true, nil)
// when this instance becomes leader, (false, nil) when another instance
// already holds the lock.
func (e *LeaderElection) Acquire(ctx context.Context) (bool, error) {
	ok, err := e.ops.SetNX(ctx, e.lockKey, e.instance, e.ttl)
	if err != nil {
		return false, fmt.Errorf("leader: setnx %s: %w", e.lockKey, err)
	}
	return ok, nil
}

// Renew refreshes the lock TTL. It first verifies ownership via GET,
// then extends the lock with SET. Returns (false, nil) if another
// instance has stolen the lock.
func (e *LeaderElection) Renew(ctx context.Context) (bool, error) {
	current, err := e.ops.Get(ctx, e.lockKey)
	if err != nil {
		return false, fmt.Errorf("leader: get %s: %w", e.lockKey, err)
	}
	if current != e.instance {
		return false, nil
	}

	if e.ops.Set != nil {
		_, err = e.ops.Set(ctx, e.lockKey, e.instance, e.ttl)
		if err != nil {
			return false, fmt.Errorf("leader: set %s: %w", e.lockKey, err)
		}
		return true, nil
	}

	// Fallback: SET NX is a best-effort renew (only works if the key
	// was deleted since the GET above).
	ok, err := e.ops.SetNX(ctx, e.lockKey, e.instance, e.ttl)
	if err != nil {
		return false, fmt.Errorf("leader: renew setnx %s: %w", e.lockKey, err)
	}
	return ok, nil
}

// Release deletes the lock key with CAS semantics via a Lua script
// (DEL-if-match) when the ops support it. Returns (false, nil) if
// this instance was not the leader.
func (e *LeaderElection) Release(ctx context.Context) (bool, error) {
	if e.ops.DelIfMatch != nil {
		return e.ops.DelIfMatch(ctx, e.lockKey, e.instance)
	}

	// Fallback: compare-then-delete (not atomic).
	current, err := e.ops.Get(ctx, e.lockKey)
	if err != nil {
		return false, fmt.Errorf("leader: get %s: %w", e.lockKey, err)
	}
	if current != e.instance {
		return false, nil
	}
	n, err := e.ops.Del(ctx, e.lockKey)
	if err != nil {
		return false, fmt.Errorf("leader: del %s: %w", e.lockKey, err)
	}
	return n == 1, nil
}

// IsLeader returns true when this instance currently holds the lock.
func (e *LeaderElection) IsLeader() bool {
	val, _ := e.ops.Get(context.Background(), e.lockKey)
	return val == e.instance
}

// LeaderID returns the current lock value (the owning instance's ID).
// Returns an empty string when no instance holds the lock.
func (e *LeaderElection) LeaderID() string {
	val, _ := e.ops.Get(context.Background(), e.lockKey)
	return val
}

// LockTTL returns the configured lock TTL.
func (e *LeaderElection) LockTTL() time.Duration { return e.ttl }

// ErrLostLeadership is returned by RenewLoop when the leader lock is stolen
// by another instance.
var ErrLostLeadership = fmt.Errorf("leader: lost leadership")

// RenewLoop periodically calls Renew until the context is cancelled
// or this instance loses the lock. Returns the context error (usually
// context.Canceled) or ErrLostLeadership when the lock is stolen.
func (e *LeaderElection) RenewLoop(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ok, err := e.Renew(ctx)
			if err != nil {
				return fmt.Errorf("leader: renew loop error: %w", err)
			}
			if !ok {
				return ErrLostLeadership
			}
		}
	}
}
