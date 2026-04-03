package redis

import (
	"testing"
	"time"

	"github.com/go-lynx/lynx-redis/conf"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestBuildUniversalOptions_UsesPreferredLifecycleFields(t *testing.T) {
	plugin := &PlugRedis{
		conf: &conf.Redis{
			Addrs:           []string{"localhost:6379"},
			MinIdleConns:    2,
			MaxIdleConns:    4,
			MaxActiveConns:  8,
			ConnMaxIdleTime: durationpb.New(45 * time.Second),
			ConnMaxLifetime: durationpb.New(5 * time.Minute),
			DialTimeout:     durationpb.New(5 * time.Second),
			ReadTimeout:     durationpb.New(3 * time.Second),
			WriteTimeout:    durationpb.New(3 * time.Second),
			PoolTimeout:     durationpb.New(4 * time.Second),
			MinRetryBackoff: durationpb.New(8 * time.Millisecond),
			MaxRetryBackoff: durationpb.New(512 * time.Millisecond),
		},
	}

	options := plugin.buildUniversalOptions()
	if options.MaxIdleConns != 4 {
		t.Fatalf("expected MaxIdleConns to be 4, got %d", options.MaxIdleConns)
	}
	if options.MaxActiveConns != 8 {
		t.Fatalf("expected MaxActiveConns to be 8, got %d", options.MaxActiveConns)
	}
	if options.ConnMaxIdleTime != 45*time.Second {
		t.Fatalf("expected ConnMaxIdleTime to be 45s, got %v", options.ConnMaxIdleTime)
	}
	if options.ConnMaxLifetime != 5*time.Minute {
		t.Fatalf("expected ConnMaxLifetime to be 5m, got %v", options.ConnMaxLifetime)
	}
}

func TestBuildUniversalOptions_FallsBackToLegacyLifecycleAliases(t *testing.T) {
	plugin := &PlugRedis{
		conf: &conf.Redis{
			Addrs:           []string{"localhost:6379"},
			MaxActiveConns:  8,
			DialTimeout:     durationpb.New(5 * time.Second),
			ReadTimeout:     durationpb.New(3 * time.Second),
			WriteTimeout:    durationpb.New(3 * time.Second),
			PoolTimeout:     durationpb.New(4 * time.Second),
			IdleTimeout:     durationpb.New(30 * time.Second),
			MaxConnAge:      durationpb.New(10 * time.Minute),
			MinRetryBackoff: durationpb.New(8 * time.Millisecond),
			MaxRetryBackoff: durationpb.New(512 * time.Millisecond),
		},
	}

	options := plugin.buildUniversalOptions()
	if options.ConnMaxIdleTime != 30*time.Second {
		t.Fatalf("expected legacy idle_timeout to map to 30s, got %v", options.ConnMaxIdleTime)
	}
	if options.ConnMaxLifetime != 10*time.Minute {
		t.Fatalf("expected legacy max_conn_age to map to 10m, got %v", options.ConnMaxLifetime)
	}
}
