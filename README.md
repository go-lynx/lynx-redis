# Redis Plugin (UniversalClient)

This plugin is based on go-redis v9's UniversalClient, providing unified support for standalone, Cluster, and Sentinel topologies, with built-in Prometheus metrics, startup health checks, command-level instrumentation, connection pool statistics, and TLS support.

Note: Configuration is delivered through protobuf (`conf/redis.proto`), and the deprecated field `addr` has been removed, keeping only `addrs`. All comments are in English.

## Feature Overview
- Supports three topologies: single / cluster / sentinel (auto-detection, or determined by `sentinel.master_name`)
- TLS support: `tls.enabled` and `tls.insecure_skip_verify`, with automatic TLS enablement for `rediss://` address prefixes
- Command-level Prometheus metrics: latency histograms, error counts
- Connection pool metrics: hits, misses, wait timeouts, idle/active/stale connection counts
- Startup and health checks: startup Ping, latency logging; enhanced readiness checks (cluster status, role/connected_slaves)
- Compatible API: `GetRedis()` (returns redis.UniversalClient) and `GetUniversalRedis()`
- **Configuration Validation**: Complete configuration validation logic to ensure correctness and reasonableness

## Configuration Description (protobuf)

See `conf/redis.proto`. All fields are grouped by domain below with their protobuf field number, Go type, default value, and a YAML example.

### Basic Connection

| Field | # | Type | Default | Example |
|-------|---|------|---------|---------|
| `network` | 1 | `string` | `"tcp"` | `tcp` |
| `addrs` | 2 | `repeated string` | — (required) | `["127.0.0.1:6379"]` |
| `username` | 3 | `string` | `""` | `myuser` |
| `password` | 4 | `string` | `""` | `s3cr3t` |
| `db` | 5 | `int32` | `0` | `0` |
| `client_name` | 6 | `string` | `""` | `my-service` |

### Connection Pool & Lifecycle

| Field | # | Type | Default | Example |
|-------|---|------|---------|---------|
| `min_idle_conns` | 7 | `int32` | `0` (go-redis default) | `5` |
| `max_idle_conns` | 8 | `int32` | `0` (go-redis default) | `10` |
| `max_active_conns` | 9 | `int32` | `0` (CPU×10 per go-redis) | `50` |
| `conn_max_idle_time` | 10 | `Duration` | not set (30 min in go-redis) | `{seconds: 300}` |
| `pool_timeout` | 13 | `Duration` | not set | `{seconds: 2}` |
| `conn_max_lifetime` | 22 | `Duration` | not set (0 = unlimited) | `{seconds: 3600}` |
| `idle_timeout` _(deprecated)_ | 11 | `Duration` | alias of `conn_max_idle_time` | `{seconds: 300}` |
| `max_conn_age` _(deprecated)_ | 12 | `Duration` | alias of `conn_max_lifetime` | `{seconds: 3600}` |

> **Note**: `idle_timeout` and `max_conn_age` are retained only for backward compatibility. New configurations should use `conn_max_idle_time` and `conn_max_lifetime`. When both a field and its deprecated alias are set, they must carry the same duration value.

### Timeouts

| Field | # | Type | Default | Example |
|-------|---|------|---------|---------|
| `dial_timeout` | 14 | `Duration` | not set (go-redis default 5 s) | `{seconds: 5}` |
| `read_timeout` | 15 | `Duration` | not set (go-redis default 3 s) | `{seconds: 3}` |
| `write_timeout` | 16 | `Duration` | not set (go-redis default 3 s) | `{seconds: 3}` |

### Retry

| Field | # | Type | Default | Example |
|-------|---|------|---------|---------|
| `max_retries` | 17 | `int32` | `0` (no retry) | `3` |
| `min_retry_backoff` | 18 | `Duration` | not set (8 ms in go-redis) | `{nanos: 8000000}` |
| `max_retry_backoff` | 19 | `Duration` | not set (512 ms in go-redis) | `{nanos: 512000000}` |

### TLS

| Field | # | Type | Default | Example |
|-------|---|------|---------|---------|
| `tls.enabled` | 20.1 | `bool` | `false` | `true` |
| `tls.insecure_skip_verify` | 20.2 | `bool` | `false` | `true` _(test env only)_ |

### Sentinel Mode

| Field | # | Type | Default | Example |
|-------|---|------|---------|---------|
| `sentinel.master_name` | 21.1 | `string` | `""` (required for sentinel) | `mymaster` |
| `sentinel.addrs` | 21.2 | `repeated string` | `[]` (falls back to `addrs`) | `["10.0.0.10:26379"]` |

## Usage Examples

Assuming the runtime configuration (env/file/config center) delivers the `redis` section in protobuf corresponding structure.

- Standalone
```yaml
redis:
  network: tcp
  addrs: ["127.0.0.1:6379"]
  db: 0
  min_idle_conns: 10
  max_active_conns: 20
  dial_timeout: { seconds: 5 }
  read_timeout: { seconds: 5 }
  write_timeout: { seconds: 5 }
```

- Cluster
```yaml
redis:
  addrs: ["10.0.0.1:6379","10.0.0.2:6379","10.0.0.3:6379"]
  min_idle_conns: 20
  max_active_conns: 100
  pool_timeout: { seconds: 2 }
```

- Sentinel (recommended to configure sentinel.addrs separately; will reuse addrs if not provided)
```yaml
redis:
  addrs: ["10.0.0.10:26379","10.0.0.11:26379","10.0.0.12:26379"]
  sentinel:
    master_name: mymaster
    # addrs: ["10.0.0.10:26379","10.0.0.11:26379","10.0.0.12:26379"]
```

- TLS (either of two methods)
```yaml
redis:
  addrs: ["rediss://10.0.0.1:6379"]
  tls:
    enabled: true
    insecure_skip_verify: true  # testing environment only
```

## Configuration Validation

The Redis plugin now includes complete configuration validation functionality to ensure configuration correctness before plugin startup. Validation includes:

- **Basic Connection Validation**: address format, network type, etc.
- **Connection Pool Configuration Validation**: connection count relationships, timeout times, etc.
- **Timeout Configuration Validation**: reasonableness and relationships of various timeout times
- **Retry Configuration Validation**: reasonableness of retry counts and backoff times
- **TLS Configuration Validation**: matching of TLS enablement and address format
- **Sentinel Configuration Validation**: necessary parameters for sentinel mode

Configuration validation will be automatically executed during plugin initialization. If validation fails, the plugin will not start. For detailed validation rules and configuration templates, please refer to [VALIDATION.md](./VALIDATION.md).

## Usage in Code
- Recommended to use package-level methods to get clients (no need to hold *PlugRedis instance):
```go
import (
    "context"
    "fmt"
    rplug "github.com/go-lynx/lynx/plugins/nosql/redis"
)

func useRedis() error {
    cli := rplug.GetUniversalRedis() // redis.UniversalClient: universal for standalone/cluster/sentinel
    if cli == nil {
        return fmt.Errorf("redis plugin not initialized")
    }
    ctx := context.Background()
    return cli.Set(ctx, "k", "v", 0).Err()
}

// If only need underlying *redis.Client in standalone mode:
func useSingleClient() error {
    c := rplug.GetRedis() // *redis.Client (nil under Cluster/Sentinel)
    if c == nil {
        return nil // or return error as needed
    }
    return c.Ping(context.Background()).Err()
}
```

## File Structure and Responsibilities
- `plug.go`:
  - Complete plugin registration (init registers to global factory)
  - Provide package-level convenience methods `GetUniversalRedis()`, `GetRedis()` for getting clients
- `plugin_meta.go`: Plugin metadata constants (name, config prefix) and factory function `NewRedisClient`
- `types.go`: Define plugin instance `PlugRedis` struct and internal fields (config, UniversalClient, collection goroutine control, etc.)
- `options.go`: Logic to build go-redis `redis.UniversalOptions` from protobuf configuration
- `hooks.go`: Implement go-redis v9 Hook (command-level instrumentation: latency histograms, error counts)
- `health.go`:
  - Topology detection (single/cluster/sentinel) and address resolution
  - Startup/readiness checks (parse INFO cluster/replication), and sync metrics
  - Read version, runtime info, background info collection goroutine
- `lifecycle.go`: Plugin lifecycle (initialize resources, start tasks, cleanup, config injection, health checks)
- `metrics.go`: Prometheus metrics definition and registration (connection pool, commands, runtime info, etc.)
- `pool_stats.go`: Timed pull and report connection pool statistics (hits/misses/timeouts/idle/total/stale)
- `conf/redis.proto`: Configuration definition (protobuf), generated to `plugins/nosql/redis/conf` directory

## Health Checks and Metrics
- Ping on startup and record latency; failures will be counted and return errors
- Readiness checks:
  - Cluster: parse `INFO cluster` to determine `cluster_state:ok`
  - Standalone/sentinel: parse `INFO replication` to determine `role`, `connected_slaves`
- Metrics:
  - Connection pool: hits/misses/timeouts/idle/total/stale
  - Commands: latency histograms, error counts (tagged by command name)
  - Runtime info: redis_version, role, connected_slaves, cluster_state

## Common Issues
- `protoc-gen-go` not found
  - Solution 1: Temporarily append PATH before execution
    ```bash
    PATH="$(go env GOPATH)/bin:$PATH" make config
    ```
  - Solution 2: Execute explicit plugin path only for this plugin
    ```bash
    cd lynx
    protoc -I plugins/nosql/redis/conf -I third_party -I boot -I app \
      --plugin=protoc-gen-go=$(go env GOPATH)/bin/protoc-gen-go \
      --go_out=paths=source_relative:plugins/nosql/redis/conf \
      plugins/nosql/redis/conf/redis.proto
    ```
- `addr` field related compilation errors
  - Description: This plugin has removed `addr` (string), uniformly using `addrs` (repeated string).
  - Phenomenon: Compilation/generated code reports cannot find `addr` field or struct has no such field.
  - Handling: Change single address to array syntax; no changes needed in application code reading locations.
  - Migration example:
    - Old:
      ```yaml
      redis:
        addr: "127.0.0.1:6379"
      ```
    - New:
      ```yaml
      redis:
        addrs: ["127.0.0.1:6379"]
      ```
  - `addr` has been removed, please use `addrs` instead (can configure single address)
- Legacy lifecycle field names
  - `idle_timeout` now maps to `ConnMaxIdleTime`
  - `max_conn_age` now maps to `ConnMaxLifetime`
  - New configurations should prefer `conn_max_idle_time` and `conn_max_lifetime`

## Version and Compatibility
- go-redis v9
- Prometheus client_golang v1.18+ (already in go.mod)
- Supports standalone/cluster/sentinel simultaneously through `redis.UniversalClient`

## Developer Tips
- If further distinction between Cluster and Failover behavior is needed, can extend in `detectMode()` and `enhancedReadinessCheck()`
- Deprecated aliases are normalized during validation, so old configs continue to work while new configs can move to the preferred lifecycle field names
