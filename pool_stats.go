package redis

import (
	"time"

	"github.com/go-lynx/lynx/log"
	"github.com/redis/go-redis/v9"
)

// startPoolStatsCollector periodically collects PoolStats and reports to Prometheus.
// It must be called after statsQuit has been initialised in StartupTasks.
func (r *PlugRedis) startPoolStatsCollector() {
	r.mu.RLock()
	quit := r.statsQuit
	r.mu.RUnlock()
	if quit == nil {
		return
	}

	r.statsWG.Add(1)
	go func() {
		defer r.statsWG.Done()
		defer func() {
			if rec := recover(); rec != nil {
				log.Errorf("redis pool stats collector panic: %v", rec)
			}
		}()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-quit:
				return
			case <-ticker.C:
				r.observePoolStats()
			}
		}
	}()
	// Collect immediately once
	r.observePoolStats()
}

func (r *PlugRedis) observePoolStats() {
	client := r.getClient()
	if client == nil {
		return
	}
	// Compatible with different client types
	switch c := client.(type) {
	case *redis.Client:
		ps := c.PoolStats()
		r.setPoolStats(ps)
	case *redis.ClusterClient:
		ps := c.PoolStats()
		r.setPoolStats(ps)
	case *redis.Ring:
		ps := c.PoolStats()
		r.setPoolStats(ps)
	default:
		// Try interface assertion (some versions of UniversalClient may directly implement PoolStats method)
		type poolStater interface{ PoolStats() *redis.PoolStats }
		if pc, ok := any(client).(poolStater); ok {
			r.setPoolStats(pc.PoolStats())
		}
	}
}

func (r *PlugRedis) setPoolStats(ps *redis.PoolStats) {
	if ps == nil {
		return
	}
	redisPoolHits.Set(float64(ps.Hits))
	redisPoolMisses.Set(float64(ps.Misses))
	redisPoolTimeouts.Set(float64(ps.Timeouts))
	redisPoolTotalConns.Set(float64(ps.TotalConns))
	redisPoolIdleConns.Set(float64(ps.IdleConns))
	redisPoolStaleConns.Set(float64(ps.StaleConns))
}
