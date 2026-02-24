package reactree

import "github.com/appcd-dev/genie/pkg/hooks"

// Toggles configures optional predictability and bounding mechanisms.
// All fields default to zero values (disabled). Callers opt in by setting booleans
// and injecting the corresponding dependency.
type Toggles struct {
	EnableCriticMiddleware bool `mapstructure:"enable_critic_middleware"`
	EnableActionReflection bool `mapstructure:"enable_action_reflection"`
	EnableDryRunSimulation bool `mapstructure:"enable_dry_run_simulation"`
	EnableMCPServerAccess  bool `mapstructure:"enable_mcp_server_access"`
	EnableAuditDashboard   bool `mapstructure:"enable_audit_dashboard"`

	// Reflector is the ActionReflector used for RAR loops.
	// Only used when EnableActionReflection is true.
	Reflector ActionReflector `json:"-"`

	// Hooks are lifecycle callbacks invoked at well-defined points during
	// tree execution. Multiple hooks can be composed via hooks.NewChainHook.
	// Hooks replace the previous AuditEmitter field — the AuditHook
	// implementation provides the same audit-logging behavior.
	Hooks hooks.ExecutionHook `json:"-"`
}
