package leader

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// NewOpsFromRedis creates leader.Ops from a go-redis client.
// This bridges go-redis commands to the leader.Ops interface for production use.
func NewOpsFromRedis(client *redis.Client) Ops {
	return Ops{
		SetNX: func(ctx context.Context, key, value string, expiration time.Duration) (bool, error) {
			return client.SetNX(ctx, key, value, expiration).Result()
		},
		Get: func(ctx context.Context, key string) (string, error) {
			return client.Get(ctx, key).Result()
		},
		Set: func(ctx context.Context, key, value string, expiration time.Duration) (bool, error) {
			err := client.Set(ctx, key, value, expiration).Err()
			return err == nil, err
		},
		Del: func(ctx context.Context, keys ...string) (int64, error) {
			return client.Del(ctx, keys...).Result()
		},
		DelIfMatch: func(ctx context.Context, key, expected string) (bool, error) {
			script := `
				if redis.call("GET", KEYS[1]) == ARGV[1] then
					return redis.call("DEL", KEYS[1])
				else
					return 0
				end
			`
			result := client.Eval(ctx, script, []string{key}, expected)
			n, err := result.Int64()
			if err != nil {
				return false, err
			}
			return n == 1, nil
		},
	}
}
