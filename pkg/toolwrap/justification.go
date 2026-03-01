package toolwrap

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const _justification = "_justification"

// extractJustification pulls the optional "_justification" key from a JSON
// tool-call argument blob.
func extractJustification(args []byte) (string, []byte) {
	justification := gjson.GetBytes(args, _justification)
	if !justification.Exists() {
		return "", args
	}
	stripped, _ := sjson.DeleteBytes(args, _justification)
	return justification.String(), stripped
}
