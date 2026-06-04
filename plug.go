package redis

import (
	"context"

	"github.com/go-lynx/lynx/pkg/factory"
	"github.com/go-lynx/lynx/plugins"
	"github.com/redis/go-redis/v9"
)

// init registers the Redis plugin with the global factory so the plugin manager
// can discover and instantiate it when the package is imported.
func init() {
	factory.GlobalTypedFactory().RegisterPlugin(pluginName, confPrefix, func() plugins.Plugin {
		return NewRedisClient()
	})
}

// GetRedis returns the underlying *redis.Client for standalone mode.
// Returns nil when running in Cluster or Sentinel topology — use GetUniversalRedis instead.
func GetRedis() *redis.Client {
	client, err := GetProvider().SingleClient(context.Background())
	if err != nil {
		return nil
	}
	return client
}

// GetUniversalRedis returns the universal client, usable for single node/cluster/sentinel modes.
func GetUniversalRedis() redis.UniversalClient {
	client, err := GetProvider().UniversalClient(context.Background())
	if err != nil {
		return nil
	}
	return client
}

// GetUniversalClient returns the current universal redis client for the plugin instance.
func (r *PlugRedis) GetUniversalClient() redis.UniversalClient {
	return r.getClient()
}
