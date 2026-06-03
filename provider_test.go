package redis

import (
	"context"
	"errors"
	"strings"
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

// TestProvider_NilContextIsRejected ensures that passing a nil context is an explicit error
// rather than a silent fallback to context.Background(), which would hide caller bugs.
func TestProvider_NilContextIsRejected(t *testing.T) {
	p := GetProvider()
	//nolint:staticcheck // intentionally passing nil to verify error handling
	_, err := p.UniversalClient(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if !strings.Contains(err.Error(), "non-nil context") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestProvider_CancelledContextIsRejected ensures that an already-cancelled context
// is rejected before any plugin lookup is attempted.
func TestProvider_CancelledContextIsRejected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := GetProvider().UniversalClient(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

// TestPlugRedis_DetectMode verifies topology detection from configuration.
func TestPlugRedis_DetectMode(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *conf.Redis
		wantMode string
	}{
		{
			name:     "single node",
			cfg:      &conf.Redis{Addrs: []string{"localhost:6379"}},
			wantMode: "single",
		},
		{
			name:     "cluster",
			cfg:      &conf.Redis{Addrs: []string{"node1:6379", "node2:6379", "node3:6379"}},
			wantMode: "cluster",
		},
		{
			name: "sentinel",
			cfg: &conf.Redis{
				Addrs:    []string{"sentinel1:26379"},
				Sentinel: &conf.Redis_Sentinel{MasterName: "mymaster", Addrs: []string{"sentinel1:26379"}},
			},
			wantMode: "sentinel",
		},
		{
			name:     "nil config",
			cfg:      nil,
			wantMode: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := NewRedisClient()
			plugin.conf = tt.cfg
			mode := plugin.detectMode()
			if mode != tt.wantMode {
				t.Errorf("detectMode() = %q, want %q", mode, tt.wantMode)
			}
		})
	}
}

// TestPlugRedis_CurrentAddrList verifies that sentinel address override takes precedence.
func TestPlugRedis_CurrentAddrList(t *testing.T) {
	plugin := NewRedisClient()
	plugin.conf = &conf.Redis{
		Addrs: []string{"fallback:6379"},
		Sentinel: &conf.Redis_Sentinel{
			MasterName: "mymaster",
			Addrs:      []string{"sentinel1:26379", "sentinel2:26379"},
		},
	}
	addrs := plugin.currentAddrList()
	if len(addrs) != 2 || addrs[0] != "sentinel1:26379" {
		t.Errorf("expected sentinel addrs, got %v", addrs)
	}
}

// TestPlugRedis_StartPoolStatsCollector_NilQuit verifies that calling
// startPoolStatsCollector before statsQuit is initialised is a no-op.
func TestPlugRedis_StartPoolStatsCollector_NilQuit(t *testing.T) {
	plugin := NewRedisClient()
	// statsQuit is nil – must not panic or block
	plugin.startPoolStatsCollector()
}

// TestPlugRedis_StartInfoCollector_NilQuit verifies that calling
// startInfoCollector before statsQuit is initialised is a no-op.
func TestPlugRedis_StartInfoCollector_NilQuit(t *testing.T) {
	plugin := NewRedisClient()
	// statsQuit is nil – must not panic or block
	plugin.startInfoCollector("single")
}

// TestCacheManager_SetWithRetry_ContextCancelled verifies that SetWithRetry honours
// context cancellation during the backoff delay between retries.
func TestCacheManager_SetWithRetry_ContextCancelled(t *testing.T) {
	// Use a client that is guaranteed to fail every Set (unreachable host)
	client := goredis.NewClient(&goredis.Options{
		Addr:        "localhost:19999",
		DialTimeout: time.Millisecond,
		ReadTimeout: time.Millisecond,
	})
	cm := NewCacheManager(client, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := cm.SetWithRetry(ctx, "key", "val", time.Minute, 10)
	if err == nil {
		t.Fatal("expected error when context times out")
	}
}

// TestPlugRedis_ValidateAndSetDefaults_ZeroMaxActiveConns verifies that omitting
// MaxActiveConns does not fail validation because defaults are applied first.
func TestPlugRedis_ValidateAndSetDefaults_ZeroMaxActiveConns(t *testing.T) {
	cfg := &conf.Redis{
		Addrs: []string{"localhost:6379"},
		// MaxActiveConns intentionally omitted (zero value)
	}
	if err := ValidateAndSetDefaults(cfg); err != nil {
		t.Fatalf("expected no error when MaxActiveConns is omitted; got: %v", err)
	}
	if cfg.MaxActiveConns != 100 {
		t.Errorf("expected default MaxActiveConns=100, got %d", cfg.MaxActiveConns)
	}
}
