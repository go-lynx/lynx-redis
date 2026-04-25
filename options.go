package redis

import (
	"crypto/tls"
	"strings"
	"time"

	"github.com/go-lynx/lynx-redis/conf"
	"github.com/redis/go-redis/v9"
)

// buildUniversalOptions builds redis.UniversalOptions based on configuration
func (r *PlugRedis) buildUniversalOptions() *redis.UniversalOptions {
	cfg := r.getConfig()
	if cfg == nil {
		cfg = &conf.Redis{}
	}
	// Parse addresses: prioritize addrs; fallback to addr if empty (supports comma separation)
	var addrs []string
	if len(cfg.Addrs) > 0 {
		addrs = append(addrs, cfg.Addrs...)
	}
	// TLS: configuration priority; then rediss:// inference
	var tlsConfig *tls.Config
	if cfg.Tls != nil && cfg.Tls.Enabled {
		tlsConfig = &tls.Config{InsecureSkipVerify: cfg.Tls.InsecureSkipVerify}
	}
	for i := range addrs {
		if strings.HasPrefix(strings.ToLower(addrs[i]), "rediss://") {
			if tlsConfig == nil {
				tlsConfig = &tls.Config{}
			}
			addrs[i] = strings.TrimPrefix(addrs[i], "rediss://")
		}
	}
	// Sentinel: allow dedicated sentinel address override
	masterName := ""
	if cfg.Sentinel != nil {
		masterName = cfg.Sentinel.MasterName
		if len(cfg.Sentinel.Addrs) > 0 {
			addrs = append([]string{}, cfg.Sentinel.Addrs...)
		}
	}

	return &redis.UniversalOptions{
		Addrs:                 addrs,
		MasterName:            masterName,
		DB:                    int(cfg.Db),
		Username:              cfg.Username,
		Password:              cfg.Password,
		MinIdleConns:          int(cfg.MinIdleConns),
		MaxIdleConns:          int(cfg.MaxIdleConns),
		PoolSize:              int(cfg.MaxActiveConns),
		MaxActiveConns:        int(cfg.MaxActiveConns),
		DialTimeout:           cfg.DialTimeout.AsDuration(),
		ReadTimeout:           cfg.ReadTimeout.AsDuration(),
		WriteTimeout:          cfg.WriteTimeout.AsDuration(),
		ConnMaxIdleTime:       effectiveConnMaxIdleTime(cfg),
		PoolTimeout:           cfg.PoolTimeout.AsDuration(),
		MaxRetries:            int(cfg.MaxRetries),
		MinRetryBackoff:       cfg.MinRetryBackoff.AsDuration(),
		MaxRetryBackoff:       cfg.MaxRetryBackoff.AsDuration(),
		ClientName:            cfg.ClientName,
		TLSConfig:             tlsConfig,
		ContextTimeoutEnabled: true,
		ConnMaxLifetime:       effectiveConnMaxLifetime(cfg),
	}
}

func effectiveConnMaxIdleTime(cfg *conf.Redis) time.Duration {
	if cfg.ConnMaxIdleTime != nil {
		return cfg.ConnMaxIdleTime.AsDuration()
	}
	if cfg.IdleTimeout != nil {
		return cfg.IdleTimeout.AsDuration()
	}
	return 0
}

func effectiveConnMaxLifetime(cfg *conf.Redis) time.Duration {
	if cfg.ConnMaxLifetime != nil {
		return cfg.ConnMaxLifetime.AsDuration()
	}
	if cfg.MaxConnAge != nil {
		return cfg.MaxConnAge.AsDuration()
	}
	return 0
}
