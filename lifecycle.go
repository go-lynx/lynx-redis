package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-lynx/lynx-redis/conf"
	"github.com/go-lynx/lynx/log"
	"github.com/go-lynx/lynx/plugins"
	"github.com/redis/go-redis/v9"
)

// InitializeResources loads Redis configuration from the runtime config.
// Skips scanning when conf is pre-set (e.g. in tests).
func (r *PlugRedis) InitializeResources(rt plugins.Runtime) error {
	if err := r.BasePlugin.InitializeResources(rt); err != nil {
		return err
	}
	r.mu.Lock()
	r.rt = rt
	cfg := r.conf
	r.mu.Unlock()

	if cfg == nil {
		cfg = &conf.Redis{}
		runtimeConf := rt.GetConfig()
		if runtimeConf == nil {
			return fmt.Errorf("redis plugin requires a runtime config but none was provided")
		}
		if err := runtimeConf.Value(confPrefix).Scan(cfg); err != nil {
			return err
		}
	}

	if err := ValidateAndSetDefaults(cfg); err != nil {
		return fmt.Errorf("redis configuration validation failed: %w", err)
	}
	r.mu.Lock()
	r.conf = cfg
	r.mu.Unlock()

	return nil
}

// StartupTasks creates the universal client, runs a startup ping, then launches
// pool-stats and info collectors. It is the legacy (non-cancellable) entrypoint
// and delegates to StartupTasksContext with a background context.
func (r *PlugRedis) StartupTasks() error {
	return r.StartupTasksContext(context.Background())
}

// StartupTasksContext creates the universal client, runs a startup ping bound
// to ctx (capped at 2s), then launches pool-stats and info collectors. If ctx
// is cancelled at any step before the client is published, the client is
// closed and the ctx error is returned.
func (r *PlugRedis) StartupTasksContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("redis startup canceled before execution: %w", err)
	}

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if r.getClient() != nil {
		return fmt.Errorf("redis client already started")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("redis startup canceled: %w", err)
	}

	log.Infof("starting redis client")
	redisStartupTotal.Inc()

	client := redis.NewUniversalClient(r.buildUniversalOptions())
	client.AddHook(metricsHook{})

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	start := time.Now()
	_, err := client.Ping(pingCtx).Result()
	cancel()
	if err == nil {
		// Do not publish a client the caller has already given up on.
		err = ctx.Err()
	}
	if err != nil {
		if closeErr := client.Close(); closeErr != nil {
			log.Warnf("failed to close redis client during startup cleanup: %v", closeErr)
		}
		redisStartupFailedTotal.Inc()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("redis startup canceled during ping: %w", errors.Join(ctxErr, err))
		}
		return fmt.Errorf("redis ping failed during startup: %w", err)
	}
	latency := time.Since(start)
	redisPingLatency.Observe(latency.Seconds())

	r.mu.Lock()
	r.rdb = client
	r.statsQuit = make(chan struct{})
	r.mu.Unlock()

	mode := r.detectMode()
	log.Infof("redis client successfully started, mode=%s, addrs=%v, ping_latency=%s", mode, r.currentAddrList(), latency)

	r.publishResourceContract()
	r.enhancedReadinessCheckContext(ctx, mode)
	r.startPoolStatsCollector()
	r.startInfoCollector(mode)
	return nil
}

// CleanupTasks stops background collectors and closes the Redis client. It is
// the legacy (non-cancellable) entrypoint and delegates to CleanupTasksContext
// with a background context.
func (r *PlugRedis) CleanupTasks() error {
	return r.CleanupTasksContext(context.Background())
}

// CleanupTasksContext stops background collectors and closes the Redis client
// while honoring ctx. Waiting for the collectors is bounded by both ctx and a
// 10s cap; closing the client is bounded by ctx. When ctx expires the client is
// still closed in the background so the connections are eventually released.
func (r *PlugRedis) CleanupTasksContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("redis cleanup canceled before execution: %w", err)
	}

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.mu.Lock()
	client := r.rdb
	statsQuit := r.statsQuit
	r.rdb = nil
	r.statsQuit = nil
	r.mu.Unlock()

	if client == nil {
		return nil
	}

	if statsQuit != nil {
		close(statsQuit)
		done := make(chan struct{})
		go func() {
			r.statsWG.Wait()
			close(done)
		}()

		select {
		case <-done:
			log.Infof("redis stats collectors stopped successfully")
		case <-time.After(10 * time.Second):
			log.Warnf("timeout waiting for redis stats collectors to stop")
		case <-ctx.Done():
			log.Warnf("context canceled while waiting for redis stats collectors to stop: %v", ctx.Err())
		}
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- client.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			return plugins.NewPluginError(r.ID(), "Stop", "Failed to stop Redis client", err)
		}
	case <-ctx.Done():
		return fmt.Errorf("redis cleanup canceled while closing client (close continues in background): %w", ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("redis cleanup canceled: %w", err)
	}
	return nil
}

// Configure updates the in-memory Redis config. Connection parameters take effect
// only after a managed restart — the live client is not reconnected here.
func (r *PlugRedis) Configure(c any) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if c == nil {
		return nil
	}
	newConf, ok := c.(*conf.Redis)
	if !ok {
		return fmt.Errorf("invalid configuration type for redis plugin: expected *conf.Redis, got %T", c)
	}
	if err := ValidateAndSetDefaults(newConf); err != nil {
		return fmt.Errorf("redis configuration validation failed: %w", err)
	}
	r.mu.Lock()
	r.conf = newConf
	running := r.rdb != nil
	r.mu.Unlock()
	if running {
		log.Infof("redis configuration updated in memory; changes will apply on next restart")
	}
	return nil
}

// CheckHealth PINGs Redis with a 2-second timeout.
func (r *PlugRedis) CheckHealth() error {
	client := r.getClient()
	if client == nil {
		return fmt.Errorf("redis client not initialized")
	}

	// Fixed 2 s timeout avoids being blocked by a long idle-connection deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	_, err := client.Ping(ctx).Result()
	latency := time.Since(start)
	redisPingLatency.Observe(latency.Seconds())
	log.Infof("redis health check: addrs=%v, ping_latency=%s", r.currentAddrList(), latency)
	if err != nil {
		return err
	}
	return nil
}
