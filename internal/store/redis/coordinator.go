package redis

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/rizqme/loka/internal/controlplane/ha"
)

func init() {
	ha.RegisterFactory("redis", func(cfg ha.Config) (ha.Coordinator, error) {
		return NewCoordinator(CoordinatorConfig{
			Address:        cfg.Address,
			Password:       cfg.Password,
			SentinelMaster: cfg.SentinelMaster,
		}, slog.Default())
	})
}

// Coordinator implements ha.Coordinator using Redis.
type Coordinator struct {
	client   redis.UniversalClient
	logger   *slog.Logger
	instanceID string

	mu      sync.RWMutex
	leaders map[string]bool
}

// CoordinatorConfig configures the Redis coordinator.
type CoordinatorConfig struct {
	Address        string
	Password       string
	SentinelMaster string
	DB             int
}

// NewCoordinator creates a new Redis-backed coordinator.
func NewCoordinator(cfg CoordinatorConfig, logger *slog.Logger) (*Coordinator, error) {
	var client redis.UniversalClient

	if cfg.SentinelMaster != "" {
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    cfg.SentinelMaster,
			SentinelAddrs: []string{cfg.Address},
			Password:      cfg.Password,
			DB:            cfg.DB,
		})
	} else {
		client = redis.NewClient(&redis.Options{
			Addr:     cfg.Address,
			Password: cfg.Password,
			DB:       cfg.DB,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &Coordinator{
		client:     client,
		logger:     logger,
		instanceID: uuid.New().String(),
		leaders:    make(map[string]bool),
	}, nil
}

func (c *Coordinator) Lock(ctx context.Context, key string, ttl time.Duration) (func(), error) {
	lockKey := "loka:lock:" + key
	lockVal := c.instanceID + ":" + uuid.New().String()

	// Try to acquire with SETNX.
	ok, err := c.client.SetNX(ctx, lockKey, lockVal, ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("redis setnx: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("lock %s already held", key)
	}

	unlock := func() {
		// Only delete if we still hold the lock (compare-and-delete via Lua).
		script := redis.NewScript(`
			if redis.call("get", KEYS[1]) == ARGV[1] then
				return redis.call("del", KEYS[1])
			else
				return 0
			end
		`)
		script.Run(context.Background(), c.client, []string{lockKey}, lockVal)
	}

	return unlock, nil
}

func (c *Coordinator) Publish(ctx context.Context, topic string, payload []byte) error {
	return c.client.Publish(ctx, "loka:"+topic, payload).Err()
}

func (c *Coordinator) Subscribe(ctx context.Context, topic string) (<-chan []byte, error) {
	sub := c.client.Subscribe(ctx, "loka:"+topic)
	ch := make(chan []byte, 64)

	go func() {
		defer close(ch)
		msgCh := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				sub.Close()
				return
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				select {
				case ch <- []byte(msg.Payload):
				default:
				}
			}
		}
	}()

	return ch, nil
}

func (c *Coordinator) ElectLeader(ctx context.Context, name string, leaderFunc func(ctx context.Context)) error {
	lockKey := "loka:leader:" + name
	ttl := 10 * time.Second
	renewInterval := 3 * time.Second

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Try to become leader.
		lockVal := c.instanceID
		ok, err := c.client.SetNX(ctx, lockKey, lockVal, ttl).Result()
		if err != nil {
			c.logger.Warn("leader election error", "name", name, "error", err)
			time.Sleep(renewInterval)
			continue
		}

		if !ok {
			// Not leader — wait and retry.
			time.Sleep(renewInterval)
			continue
		}

		// We are the leader.
		c.mu.Lock()
		c.leaders[name] = true
		c.mu.Unlock()
		c.logger.Info("elected leader", "name", name, "instance", c.instanceID)

		leaderCtx, cancel := context.WithCancel(ctx)

		// Renew lock in background.
		done := make(chan struct{})
		go func() {
			defer close(done)
			ticker := time.NewTicker(renewInterval)
			defer ticker.Stop()
			for {
				select {
				case <-leaderCtx.Done():
					return
				case <-ticker.C:
					// Renew only if we still hold it.
					val, err := c.client.Get(leaderCtx, lockKey).Result()
					if err != nil || val != lockVal {
						cancel()
						return
					}
					c.client.Expire(leaderCtx, lockKey, ttl)
				}
			}
		}()

		leaderFunc(leaderCtx)

		cancel()
		<-done

		c.mu.Lock()
		c.leaders[name] = false
		c.mu.Unlock()

		// Release lock.
		val, _ := c.client.Get(ctx, lockKey).Result()
		if val == lockVal {
			c.client.Del(ctx, lockKey)
		}

		c.logger.Info("lost leadership", "name", name)
	}
}

func (c *Coordinator) IsLeader(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.leaders[name]
}

func (c *Coordinator) Close() error {
	return c.client.Close()
}

var _ ha.Coordinator = (*Coordinator)(nil)
