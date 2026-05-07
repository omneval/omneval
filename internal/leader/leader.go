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

// Ops holds the Redis operations used by leader election.
// This allows injecting a fake implementation in tests.
type Ops struct {
	// SetNX attempts to set a key to value if it does not exist.
	// Returns (true, nil) if the key was set, (false, nil) if the key already existed.
	SetNX func(ctx context.Context, key, value string, expiration time.Duration) (bool, error)
	// Get retrieves the value of a key. Returns ("", nil) if the key does not exist.
	Get func(ctx context.Context, key string) (string, error)
	// Set sets a key to value, overwriting any existing value.
	// Returns (true, nil) if the key was set.
	Set func(ctx context.Context, key, value string, expiration time.Duration) (bool, error)
	// Del removes the specified keys. Returns the number of keys that were removed.
	Del func(ctx context.Context, keys ...string) (int64, error)
	// DelIfMatch removes the key only if its current value matches expected.
	// Returns (true, nil) if the key was deleted, (false, nil) if the value didn't match.
	DelIfMatch func(ctx context.Context, key, expected string) (bool, error)
}

// LeaderElection provides Redis-based leader election using the SET NX PX
// lock pattern. Only one instance holds the lock at a time.
type LeaderElection struct {
	ops      Ops
	lockKey  string
	instance string
	ttl      time.Duration
}

// NewLeaderElection creates a new LeaderElection instance.
// ops: Redis operations (use go-redis client ops for production).
// lockKey: the Redis key used for the lock.
// instance: unique identity of this Writer Service replica.
// ttl: how long the lock is valid (default 15s).
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

// Acquire attempts to acquire the leader lock.
// Returns (true, nil) if this instance became the leader.
// Returns (false, nil) if the lock is already held by another instance.
func (e *LeaderElection) Acquire(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	ok, err := e.ops.SetNX(ctx, e.lockKey, e.instance, e.ttl)
	if err != nil {
		return false, fmt.Errorf("leader: setnx %s: %w", e.lockKey, err)
	}
	return ok, nil
}

// Renew attempts to renew the leader lock by refreshing its TTL.
// Uses SET (overwrite) to extend the lock, but verifies ownership first.
// Returns (true, nil) if the lock was renewed successfully.
// Returns (false, nil) if this instance is no longer the leader.
func (e *LeaderElection) Renew(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// Verify we still hold the lock before refreshing.
	current, err := e.ops.Get(ctx, e.lockKey)
	if err != nil {
		return false, fmt.Errorf("leader: get %s: %w", e.lockKey, err)
	}
	if current != e.instance {
		return false, nil
	}

	// Refresh TTL only if we still own the lock.
	if e.ops.Set != nil {
		_, err = e.ops.Set(ctx, e.lockKey, e.instance, e.ttl)
		if err != nil {
			return false, fmt.Errorf("leader: set %s: %w", e.lockKey, err)
		}
		return true, nil
	}

	// Fallback: use SET NX as a best-effort renew.
	// This only works if the key was somehow deleted (unlikely).
	ok, err := e.ops.SetNX(ctx, e.lockKey, e.instance, e.ttl)
	if err != nil {
		return false, fmt.Errorf("leader: renew setnx %s: %w", e.lockKey, err)
	}
	return ok, nil
}

// Release releases the leader lock by deleting the lock key.
// Only succeeds if this instance currently holds the lock (CAS semantics).
// Returns (true, nil) if the lock was released.
// Returns (false, nil) if this instance was not the leader.
func (e *LeaderElection) Release(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// Use DelIfMatch for CAS semantics if available.
	if e.ops.DelIfMatch != nil {
		return e.ops.DelIfMatch(ctx, e.lockKey, e.instance)
	}

	// Fallback: check value, then del.
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

// IsLeader returns true if this instance currently holds the leader lock.
func (e *LeaderElection) IsLeader() bool {
	val, _ := e.ops.Get(context.Background(), e.lockKey)
	return val == e.instance
}

// LeaderID returns the current leader's instance ID.
// Returns the owning instance's ID if this instance is leader,
// returns the other instance's ID if a different instance is leader,
// returns empty string if no one is leader.
func (e *LeaderElection) LeaderID() string {
	val, _ := e.ops.Get(context.Background(), e.lockKey)
	return val
}

// LockTTL returns the configured lock TTL.
func (e *LeaderElection) LockTTL() time.Duration {
	return e.ttl
}

// RenewLoop starts a background loop that periodically renews the
// leader lock until the context is cancelled.
// interval: how often to attempt renewal.
// Returns an error if the instance loses leadership during the loop.
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
				return fmt.Errorf("leader: lost leadership")
			}
		}
	}
}
