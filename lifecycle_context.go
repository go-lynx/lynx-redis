package redis

// IsContextAware asserts that the plugin's lifecycle genuinely observes context
// cancellation. The core BasePlugin drives StartContext/StopContext and routes
// into StartupTasksContext / CleanupTasksContext (see lifecycle.go), which bind
// the startup PING, readiness probes, collector shutdown and client close to
// the caller's context.
func (r *PlugRedis) IsContextAware() bool {
	return true
}
