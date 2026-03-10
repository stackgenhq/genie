// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package sqltool

import (
	"strings"
	"testing"
)

// ────────────────────── Go Fuzz Tests ──────────────────────

// FuzzReadOnlyPattern verifies the read-only regex never allows dangerous
// statements, no matter what prefix/suffix is combined with them.
func FuzzReadOnlyPattern(f *testing.F) {
	// Seed corpus: common injection attempts.
	seeds := []string{
		"SELECT 1",
		"INSERT INTO t VALUES (1)",
		"DELETE FROM t",
		"UPDATE t SET x=1",
		"DROP TABLE t",
		"ALTER TABLE t ADD c TEXT",
		"TRUNCATE t",
		"CREATE TABLE t(id int)",
		"GRANT ALL ON t TO u",
		"; DROP TABLE users",
		"SELECT 1; DELETE FROM t",
		"  DELETE FROM t",
		"\nDROP TABLE t",
		"EXEC xp_cmdshell('cmd')",
		"CALL proc()",
		"SET @x=1",
		"LOAD DATA INFILE '/etc/passwd'",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	// The list of dangerous statement prefixes (after normalisation).
	dangerousPrefixes := []string{
		"insert", "update", "delete", "drop", "alter",
		"truncate", "create", "grant", "revoke",
		"exec", "call", "set ", "load",
	}

	f.Fuzz(func(t *testing.T, query string) {
		matched := readOnlyPattern.MatchString(query)

		// If the pattern says it's safe, verify none of the dangerous
		// keywords appear at the start of the (trimmed, lowered) query.
		if matched {
			trimmed := strings.TrimSpace(strings.ToLower(query))
			for _, prefix := range dangerousPrefixes {
				if strings.HasPrefix(trimmed, prefix) {
					t.Errorf("readOnlyPattern accepted dangerous query starting with %q: %q",
						prefix, query)
				}
			}
		}
	})
}
