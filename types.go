package redis

import (
	"sync"

	"github.com/go-lynx/lynx-redis/conf"
	"github.com/go-lynx/lynx/plugins"
	"github.com/redis/go-redis/v9"
)

// PlugRedis represents a Redis plugin instance
type PlugRedis struct {
	// Inherits from base plugin
	*plugins.BasePlugin
	// Redis configuration
	conf *conf.Redis
	// Redis client instance (supports single node/cluster/sentinel)
	rdb redis.UniversalClient
	// Runtime reference for registering shared resource (set in InitializeResources)
	rt plugins.Runtime
	// Metrics collection
	statsQuit   chan struct{}
	statsWG     sync.WaitGroup
	mu          sync.RWMutex
	lifecycleMu sync.Mutex
}

func (r *PlugRedis) getClient() redis.UniversalClient {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rdb
}

func (r *PlugRedis) setClient(client redis.UniversalClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rdb = client
}

func (r *PlugRedis) getConfig() *conf.Redis {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.conf
}

func (r *PlugRedis) getRuntime() plugins.Runtime {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rt
}

// GetPoolStats returns the connection pool statistics
func (r *PlugRedis) GetPoolStats() *redis.PoolStats {
	client := r.getClient()
	if client == nil {
		return nil
	}

	// Compatible with different client types
	switch c := client.(type) {
	case *redis.Client:
		return c.PoolStats()
	case *redis.ClusterClient:
		return c.PoolStats()
	case *redis.Ring:
		return c.PoolStats()
	default:
		// Try interface assertion (some versions of UniversalClient may directly implement PoolStats method)
		type poolStater interface{ PoolStats() *redis.PoolStats }
		if pc, ok := any(client).(poolStater); ok {
			return pc.PoolStats()
		}
	}
	return nil
}

// HealthCheck performs a health check on the Redis connection
func (r *PlugRedis) HealthCheck() (bool, error) {
	err := r.CheckHealth()
	return err == nil, err
}
