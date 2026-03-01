package toolwrap

import (
	"time"

	"github.com/stackgenhq/genie/pkg/cron"
)

// RedactSensitiveArgsForTest exports redactSensitiveArgs for tests only. Not part of the public API.
var RedactSensitiveArgsForTest = redactSensitiveArgs

// ExtractJustificationForTest exports extractJustification for tests only. Not part of the public API.
// Returns (justification, strippedArgs, found).
var ExtractJustificationForTest = extractJustification

// SemanticKeyForTest creates a temporary semanticCacheMiddleware and returns its key for tests only. Not part of the public API.
func SemanticKeyForTest(toolName string, args []byte) (string, bool) {
	m := &semanticCacheMiddleware{
		keyFields: map[string][]string{
			cron.ToolName: {"name"},
		},
	}
	return m.semanticKey(toolName, args)
}

// NewSharedHITLCacheForTest creates a WithSharedApprovalCache option for tests only (long TTL). Not part of the public API.
func NewSharedHITLCacheForTest() HITLOption {
	return WithSharedApprovalCache(newApprovalCache(time.Hour))
}
