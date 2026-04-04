package redis

import (
	"context"
	"testing"
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
