package toolwrap

// RedactSensitiveArgsForTest exports redactSensitiveArgs for external tests.
var RedactSensitiveArgsForTest = redactSensitiveArgs

// SemanticKeyForTest creates a temporary semanticCacheMiddleware with the
// given tool/fields and tests key generation.
func SemanticKeyForTest(toolName string, args []byte) (string, bool) {
	m := &semanticCacheMiddleware{
		keyFields: map[string][]string{
			"create_recurring_task": {"name"},
		},
	}
	return m.semanticKey(toolName, args)
}

// NewSharedHITLCacheForTest creates a WithSharedApprovalCache option backed
// by a fresh approval cache. Pass the same option to multiple
// HITLApprovalMiddleware calls to verify cross-middleware approval sharing.
func NewSharedHITLCacheForTest() HITLOption {
	return WithSharedApprovalCache(newApprovalCache())
}
