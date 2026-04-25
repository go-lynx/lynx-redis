package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/go-lynx/lynx-redis/conf"
	"github.com/go-lynx/lynx/log"
	"github.com/go-lynx/lynx/plugins"
	"github.com/redis/go-redis/v9"
)

// InitializeResources implements custom initialization logic for Redis plugin
// Scans and loads Redis configuration from runtime config, uses default config if not provided
// Parameter rt is the runtime environment
// Returns error information, returns corresponding error if configuration loading fails
func (r *PlugRedis) InitializeResources(rt plugins.Runtime) error {
	if err := r.BasePlugin.InitializeResources(rt); err != nil {
		return err
	}
	r.mu.Lock()
	r.rt = rt
	cfg := r.conf
	r.mu.Unlock()

	// Scan config from runtime only when not pre-set (e.g. in tests)
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

	// Validate configuration and set default values
	if err := ValidateAndSetDefaults(cfg); err != nil {
		return fmt.Errorf("redis configuration validation failed: %w", err)
	}
	r.mu.Lock()
	r.conf = cfg
	r.mu.Unlock()

	return nil
}

// StartupTasks starts Redis client and performs health check
// Returns error information, returns corresponding error if startup or health check fails
func (r *PlugRedis) StartupTasks() error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if r.getClient() != nil {
		return fmt.Errorf("redis client already started")
	}

	// Log Redis client startup
	log.Infof("starting redis client")

	// Increment startup counter
	redisStartupTotal.Inc()

	// Create Redis universal client (supports single node/cluster/sentinel)
	client := redis.NewUniversalClient(r.buildUniversalOptions())

	// Register command-level metrics hook
	client.AddHook(metricsHook{})

	// Perform quick health check at startup (short timeout)
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	start := time.Now()
	_, err := client.Ping(pingCtx).Result()
	cancel()
	if err != nil {
		if closeErr := client.Close(); closeErr != nil {
			log.Warnf("failed to close redis client during startup cleanup: %v", closeErr)
		}
		redisStartupFailedTotal.Inc()
		return fmt.Errorf("redis ping failed during startup: %w", err)
	}
	latency := time.Since(start)
	redisPingLatency.Observe(latency.Seconds())

	r.mu.Lock()
	r.rdb = client
	r.statsQuit = make(chan struct{})
	r.mu.Unlock()

	// Determine mode (single node/cluster/sentinel)
	mode := r.detectMode()
	log.Infof("redis client successfully started, mode=%s, addrs=%v, ping_latency=%s", mode, r.currentAddrList(), latency)

	r.publishResourceContract()

	// Perform enhanced check at startup stage
	r.enhancedReadinessCheck(mode)

	// Start pool statistics collector
	r.startPoolStatsCollector()
	// Start info collector
	r.startInfoCollector(mode)
	return nil
}

// cleanupOnStartupFailure cleans up resources on startup failure
func (r *PlugRedis) cleanupOnStartupFailure() {
	// Close Redis client
	client := r.getClient()
	if client != nil {
		if err := client.Close(); err != nil {
			log.Warnf("failed to close redis client during startup cleanup: %v", err)
		}
		r.setClient(nil)
	}

	// Clean up collector channel
	r.mu.Lock()
	statsQuit := r.statsQuit
	r.statsQuit = nil
	r.mu.Unlock()
	if statsQuit != nil {
		close(statsQuit)
	}

	// Wait for potentially started goroutines to exit
	done := make(chan struct{})
	go func() {
		r.statsWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Infof("startup cleanup completed successfully")
	case <-time.After(5 * time.Second):
		log.Warnf("timeout waiting for goroutines cleanup during startup failure")
	}
}

// CleanupTasks closes Redis client
// Returns error information, returns corresponding error if client closing fails
func (r *PlugRedis) CleanupTasks() error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	// If Redis client is not initialized, return nil directly
	r.mu.Lock()
	client := r.rdb
	statsQuit := r.statsQuit
	r.rdb = nil
	r.statsQuit = nil
	r.mu.Unlock()

	if client == nil {
		return nil
	}

	// Stop collectors
	if statsQuit != nil {
		close(statsQuit)
		// Wait for all collector goroutines to exit, set timeout to avoid infinite waiting
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
		}
	}

	// Close Redis client
	if err := client.Close(); err != nil {
		// Return error with plugin information
		return plugins.NewPluginError(r.ID(), "Stop", "Failed to stop Redis client", err)
	}
	return nil
}

// Configure allows updating Redis server configuration at runtime
// Parameter c should be a pointer to a conf.Redis structure, containing new configuration information
// Returns error information, returns corresponding error if configuration update fails
func (r *PlugRedis) Configure(c any) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	// If the incoming configuration is nil, return nil directly
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
	// Redis connections are created during startup; runtime Configure only updates the stored config
	// and the new values take effect after the next managed restart.
	r.mu.Lock()
	r.conf = newConf
	running := r.rdb != nil
	r.mu.Unlock()
	if running {
		log.Infof("redis configuration updated in memory; changes will apply on next restart")
	}
	return nil
}

// CheckHealth implements the health check interface for Redis server
// Performs necessary health checks on the Redis server and updates the provided health report
// Parameter report is a pointer to the health report, used to record health check results
// Returns error information, returns corresponding error if health check fails
func (r *PlugRedis) CheckHealth() error {
	client := r.getClient()
	if client == nil {
		return fmt.Errorf("redis client not initialized")
	}

	// Perform health check with fixed short timeout to avoid being affected by idle connection configuration
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	// Ensure context is cancelled at the end of the function
	defer cancel()

	// Execute Redis client Ping operation for health check
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
