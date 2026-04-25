package redis

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-lynx/lynx-redis/conf"
	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestProvider_ReturnsErrorWhenLynxUnavailable(t *testing.T) {
	provider := GetProvider()
	if provider == nil {
		t.Fatal("expected redis provider")
	}
	if _, err := provider.UniversalClient(context.Background()); err == nil {
		t.Fatal("expected error when lynx is not initialized")
	}
	if GetUniversalRedis() != nil {
		t.Fatal("expected nil universal redis client when lynx is not initialized")
	}
	if GetRedis() != nil {
		t.Fatal("expected nil standalone redis client when lynx is not initialized")
	}
}

func TestPlugRedis_CleanupClearsClientAndIsIdempotent(t *testing.T) {
	plugin := NewRedisClient()
	plugin.conf = &conf.Redis{Addrs: []string{"localhost:6379"}}
	plugin.setClient(goredis.NewClient(&goredis.Options{Addr: "localhost:6379"}))
	plugin.statsQuit = make(chan struct{})

	if plugin.GetUniversalClient() == nil {
		t.Fatal("expected redis client before cleanup")
	}
	if err := plugin.CleanupTasks(); err != nil {
		t.Fatalf("CleanupTasks failed: %v", err)
	}
	if plugin.GetUniversalClient() != nil {
		t.Fatal("expected redis client to be cleared after cleanup")
	}
	if err := plugin.CleanupTasks(); err != nil {
		t.Fatalf("second CleanupTasks should be idempotent: %v", err)
	}
}

func TestPlugRedis_ConcurrentConfigAndClientAccess(t *testing.T) {
	plugin := NewRedisClient()
	plugin.conf = redisTestConfig()
	plugin.setClient(goredis.NewClient(&goredis.Options{Addr: "localhost:6379"}))
	t.Cleanup(func() {
		_ = plugin.CleanupTasks()
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = plugin.GetUniversalClient()
			_ = plugin.GetPoolStats()
			_ = plugin.currentAddrList()
		}()
		go func() {
			defer wg.Done()
			_ = plugin.Configure(redisTestConfig())
		}()
	}
	wg.Wait()
}

func redisTestConfig() *conf.Redis {
	return &conf.Redis{
		Addrs:           []string{"localhost:6379"},
		MinIdleConns:    1,
		MaxIdleConns:    2,
		MaxActiveConns:  4,
		ClientName:      "client",
		DialTimeout:     durationpb.New(time.Second),
		ReadTimeout:     durationpb.New(time.Second),
		WriteTimeout:    durationpb.New(time.Second),
		PoolTimeout:     durationpb.New(time.Second),
		MinRetryBackoff: durationpb.New(time.Millisecond),
		MaxRetryBackoff: durationpb.New(10 * time.Millisecond),
	}
}

func TestRateLimiter_RejectsInvalidConfig(t *testing.T) {
	limiter := NewRateLimiter(goredis.NewClient(&goredis.Options{Addr: "localhost:6379"}), zerolog.Nop())

	if allowed, err := limiter.Allow(context.Background(), "rate:test", 0, time.Second); err == nil || allowed {
		t.Fatal("expected non-positive limit to be rejected without allowing request")
	}
	if allowed, err := limiter.Allow(context.Background(), "rate:test", 1, 0); err == nil || allowed {
		t.Fatal("expected non-positive window to be rejected without allowing request")
	}
}
