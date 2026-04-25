package redis

import (
	"context"
	"fmt"

	"github.com/go-lynx/lynx"
	"github.com/go-lynx/lynx/log"
	goredis "github.com/redis/go-redis/v9"
)

const (
	legacySharedResourceName   = "redis"
	sharedProviderResourceName = pluginName + ".provider"
	privateClientResourceName  = "client"
	privateConfigResourceName  = "config"
	privateProviderResource    = "provider"
)

// Provider resolves the current Redis handles on demand so long-lived callers can avoid caching
// a replaceable raw client as a singleton dependency.
type Provider interface {
	UniversalClient(ctx context.Context) (goredis.UniversalClient, error)
	SingleClient(ctx context.Context) (*goredis.Client, error)
	Mode(ctx context.Context) (string, error)
}

type provider struct{}

func getPlugin() (*PlugRedis, error) {
	app := lynx.Lynx()
	if app == nil {
		return nil, fmt.Errorf("lynx not initialized")
	}
	manager := app.GetPluginManager()
	if manager == nil {
		return nil, fmt.Errorf("plugin manager not initialized")
	}
	plugin := manager.GetPlugin(pluginName)
	if plugin == nil {
		return nil, fmt.Errorf("plugin %s not found", pluginName)
	}
	client, ok := plugin.(*PlugRedis)
	if !ok {
		return nil, fmt.Errorf("plugin %s is not a PlugRedis", pluginName)
	}
	return client, nil
}

// GetProvider returns the stable Redis provider.
func GetProvider() Provider {
	return provider{}
}

func (provider) UniversalClient(ctx context.Context) (goredis.UniversalClient, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	plugin, err := getPlugin()
	if err != nil {
		return nil, err
	}
	client := plugin.getClient()
	if client == nil {
		return nil, fmt.Errorf("redis client is nil")
	}
	return client, nil
}

func (p provider) SingleClient(ctx context.Context) (*goredis.Client, error) {
	client, err := p.UniversalClient(ctx)
	if err != nil {
		return nil, err
	}
	single, ok := client.(*goredis.Client)
	if !ok {
		return nil, fmt.Errorf("redis plugin is not running in standalone mode")
	}
	return single, nil
}

func (p provider) Mode(ctx context.Context) (string, error) {
	plugin, err := getPlugin()
	if err != nil {
		return "", err
	}
	return plugin.detectMode(), nil
}

func (r *PlugRedis) publishResourceContract() {
	client := r.getClient()
	rt := r.getRuntime()
	if r == nil || rt == nil || client == nil {
		return
	}

	redisProvider := GetProvider()
	// Keep legacy raw-client shared resources for existing plugins while publishing the stable provider resource.
	for _, resourceName := range []string{legacySharedResourceName, pluginName} {
		if err := rt.RegisterSharedResource(resourceName, client); err != nil {
			log.Warnf("failed to register redis shared resource %s: %v", resourceName, err)
		}
	}
	for _, resourceName := range []string{"redis.provider", sharedProviderResourceName} {
		if err := rt.RegisterSharedResource(resourceName, redisProvider); err != nil {
			log.Warnf("failed to register redis provider resource %s: %v", resourceName, err)
		}
	}
	if err := rt.RegisterPrivateResource(privateClientResourceName, client); err != nil {
		log.Warnf("failed to register redis private client resource: %v", err)
	}
	if err := rt.RegisterPrivateResource(privateProviderResource, redisProvider); err != nil {
		log.Warnf("failed to register redis private provider resource: %v", err)
	}
	if cfg := r.getConfig(); cfg != nil {
		if err := rt.RegisterPrivateResource(privateConfigResourceName, cfg); err != nil {
			log.Warnf("failed to register redis private config resource: %v", err)
		}
	}
}
