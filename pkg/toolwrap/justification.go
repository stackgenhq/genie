package toolwrap

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const _justification = "_justification"

// extractJustification pulls the optional "_justification" key from a JSON
// tool-call argument blob. The returned found flag is true when the key exists
// in the JSON — even if the value is an empty string. This lets callers always
// strip the key when it is present. If deletion fails, the original args are
// returned unchanged so downstream tools still receive valid input.
func extractJustification(args []byte) (string, []byte, bool) {
	justification := gjson.GetBytes(args, _justification)
	if !justification.Exists() {
		return "", args, false
	}
	stripped, err := sjson.DeleteBytes(args, _justification)
	if err != nil {
		// Deletion failed — return original args to avoid propagating nil.
		return justification.String(), args, true
	}
	return justification.String(), stripped, true
}
