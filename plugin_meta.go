package redis

import (
	"github.com/go-lynx/lynx/plugins"
)

const (
	pluginName        = "redis.client"
	pluginVersion     = "v1.6.1"
	pluginDescription = "redis plugin for lynx framework"
	confPrefix        = "lynx.redis"
)

// NewRedisClient creates a new Redis plugin instance.
func NewRedisClient() *PlugRedis {
	return &PlugRedis{
		BasePlugin: plugins.NewBasePlugin(
			plugins.GeneratePluginID("", pluginName, pluginVersion),
			pluginName,
			pluginDescription,
			pluginVersion,
			confPrefix,
			100,
		),
	}
}
