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
