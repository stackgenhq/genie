package calendar

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCalendar(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Calendar Tool Suite")
}

// mustParseTime parses an RFC3339 time string or panics. Test-only helper.
func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic("mustParseTime: " + err.Error())
	}
	return t
}
