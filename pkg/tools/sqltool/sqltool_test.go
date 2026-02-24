package sqltool

import (
	"context"

	"github.com/appcd-dev/genie/pkg/security/securityfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL Tool", func() {
	var (
		s                  *sqlTools
		fakeSecretProvider *securityfakes.FakeSecretProvider
	)

	BeforeEach(func() {
		fakeSecretProvider = &securityfakes.FakeSecretProvider{}
		s = NewSQLTools("test_db", fakeSecretProvider)
	})

	Describe("request validation", func() {
		It("rejects empty query", func() {
			err := sqlRequest{Query: "", DSN: "test"}.validate()
			Expect(err).To(MatchError(ContainSubstring("query is required")))
		})

		It("rejects empty DSN", func() {
			err := sqlRequest{Query: "SELECT 1", DSN: ""}.validate()
			Expect(err).To(MatchError(ContainSubstring("dsn is required")))
		})

		It("accepts SELECT", func() {
			err := sqlRequest{Query: "SELECT 1", DSN: "test"}.validate()
			Expect(err).NotTo(HaveOccurred())
		})

		It("accepts SHOW", func() {
			err := sqlRequest{Query: "SHOW TABLES", DSN: "test"}.validate()
			Expect(err).NotTo(HaveOccurred())
		})

		It("accepts DESCRIBE", func() {
			err := sqlRequest{Query: "DESCRIBE users", DSN: "test"}.validate()
			Expect(err).NotTo(HaveOccurred())
		})

		It("accepts EXPLAIN", func() {
			err := sqlRequest{Query: "EXPLAIN SELECT 1", DSN: "test"}.validate()
			Expect(err).NotTo(HaveOccurred())
		})

		It("accepts WITH (CTE)", func() {
			err := sqlRequest{Query: "WITH cte AS (SELECT 1) SELECT * FROM cte", DSN: "test"}.validate()
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("read-only enforcement", func() {
		mutations := []struct {
			name  string
			query string
		}{
			{"INSERT", "INSERT INTO users VALUES (1, 'test')"},
			{"UPDATE", "UPDATE users SET name='hack'"},
			{"DELETE", "DELETE FROM users"},
			{"DROP TABLE", "DROP TABLE users"},
			{"ALTER TABLE", "ALTER TABLE users ADD COLUMN hack TEXT"},
			{"TRUNCATE", "TRUNCATE TABLE users"},
			{"CREATE TABLE", "CREATE TABLE hack (id INT)"},
			{"GRANT", "GRANT ALL ON users TO hacker"},
			{"REVOKE", "REVOKE ALL ON users FROM hacker"},
			{"CREATE INDEX", "CREATE INDEX idx ON users(name)"},
			{"DROP INDEX", "DROP INDEX idx"},
		}

		for _, tc := range mutations {
			tc := tc // capture
			It("rejects "+tc.name, func() {
				err := sqlRequest{Query: tc.query, DSN: "test"}.validate()
				Expect(err).To(MatchError(ContainSubstring("read-only")))
			})
		}
	})

	Describe("SQL injection patterns", func() {
		// Category 1: Queries that do NOT start with a read-only keyword.
		// The regex MUST reject these.
		DescribeTable("rejects direct injections",
			func(query string) {
				err := sqlRequest{Query: query, DSN: "test"}.validate()
				Expect(err).To(HaveOccurred(), "injection pattern %q should be rejected", query)
			},
			Entry("EXEC xp_cmdshell", "EXEC xp_cmdshell('rm -rf /')"),
			Entry("CALL procedure", "CALL dangerous_proc()"),
			Entry("SET variable", "SET @x = 1"),
			Entry("LOAD DATA", "LOAD DATA INFILE '/etc/passwd' INTO TABLE t"),
			Entry("COPY", "COPY users TO '/tmp/data'"),
		)

		// Category 2a: Queries containing semicolons (multi-statement) are explicitly rejected.
		DescribeTable("rejects multi-statement queries",
			func(query string) {
				err := sqlRequest{Query: query, DSN: "test"}.validate()
				Expect(err).To(MatchError(ContainSubstring("multi-statement queries are not allowed")))
			},
			Entry("semicolon INSERT", "SELECT 1; INSERT INTO users VALUES (1)"),
			Entry("stacked query DELETE", "SELECT 1; DELETE FROM users"),
			Entry("encoded DROP", "SELECT 1; DROP/**/TABLE/**/users"),
		)

		// Category 2b: Queries that don't contain semicolons but include mutations (like UNION or comments).
		// The regex only checks the FIRST token, so these pass validation.
		// Protection comes from CLI single-statement execution or the DB engine.
		DescribeTable("allows stacked queries without semicolons (regex limitation)",
			func(query string) {
				err := sqlRequest{Query: query, DSN: "test"}.validate()
				Expect(err).NotTo(HaveOccurred())
			},
			Entry("UNION DROP", "SELECT 1 UNION ALL DROP TABLE users"),
			Entry("comment bypass INSERT", "SELECT 1 -- \nINSERT INTO users VALUES (1)"),
		)

		// Category 3: Queries that look suspicious but are legitimately safe
		DescribeTable("allows safe queries",
			func(query string) {
				err := sqlRequest{Query: query, DSN: "test"}.validate()
				Expect(err).NotTo(HaveOccurred(), "safe query %q should be accepted but was rejected", query)
			},
			Entry("SELECT with WHERE", "SELECT * FROM users WHERE name = 'admin'"),
			Entry("SELECT with subquery", "SELECT * FROM users WHERE id IN (SELECT id FROM orders)"),
			Entry("EXPLAIN ANALYZE", "EXPLAIN ANALYZE SELECT * FROM users"),
			Entry("SELECT with UNION", "SELECT a FROM t1 UNION SELECT b FROM t2"),
			Entry("WITH CTE", "WITH active AS (SELECT * FROM users WHERE active=1) SELECT * FROM active"),
			Entry("DESC shorthand", "DESC users"),
			Entry("SHOW DATABASES", "SHOW DATABASES"),
			Entry("tautology OR 1=1 (safe read)", "SELECT * FROM users WHERE 1=1 OR 1=1"),
			Entry("hex literal in SELECT", "SELECT 0x44524f50207461626c65"),
			Entry("INTO OUTFILE (starts with SELECT)", "SELECT * INTO OUTFILE '/tmp/hack' FROM users"),
		)
	})

	Describe("read-only pattern edge cases", func() {
		It("case insensitive SELECT", func() {
			Expect(readOnlyPattern.MatchString("select 1")).To(BeTrue())
			Expect(readOnlyPattern.MatchString("SeLeCt 1")).To(BeTrue())
		})

		It("leading whitespace", func() {
			Expect(readOnlyPattern.MatchString("  SELECT 1")).To(BeTrue())
			Expect(readOnlyPattern.MatchString("\t\nSELECT 1")).To(BeTrue())
		})

		It("blocks empty string", func() {
			Expect(readOnlyPattern.MatchString("")).To(BeFalse())
		})

		It("blocks only whitespace", func() {
			Expect(readOnlyPattern.MatchString("   ")).To(BeFalse())
		})

		It("blocks sleep injection", func() {
			Expect(readOnlyPattern.MatchString("SLEEP(10)")).To(BeFalse())
		})

		It("blocks BENCHMARK", func() {
			Expect(readOnlyPattern.MatchString("BENCHMARK(100000, SHA1('test'))")).To(BeFalse())
		})
	})

	Describe("database type normalization", func() {
		It("defaults to postgresql", func() {
			Expect(sqlRequest{Database: ""}.dbType()).To(Equal("postgresql"))
		})

		It("normalizes 'postgres'", func() {
			Expect(sqlRequest{Database: "postgres"}.dbType()).To(Equal("postgres"))
		})

		It("normalizes 'PG' (case insensitive)", func() {
			Expect(sqlRequest{Database: "PG"}.dbType()).To(Equal("pg"))
		})

		It("normalizes 'SQLite3'", func() {
			Expect(sqlRequest{Database: "SQLite3"}.dbType()).To(Equal("sqlite3"))
		})

		It("preserves unknown types for error reporting", func() {
			Expect(sqlRequest{Database: "mongodb"}.dbType()).To(Equal("mongodb"))
		})
	})

	Describe("queryWithLimit", func() {
		It("appends LIMIT when missing", func() {
			q := sqlRequest{Query: "SELECT * FROM users"}.queryWithLimit()
			Expect(q).To(ContainSubstring("LIMIT"))
		})

		It("preserves existing LIMIT", func() {
			q := sqlRequest{Query: "SELECT * FROM users LIMIT 10"}.queryWithLimit()
			Expect(q).To(ContainSubstring("LIMIT 10"))
			// Should not have double LIMIT
			Expect(q).NotTo(MatchRegexp(`LIMIT.*LIMIT`))
		})
	})

	Describe("end-to-end query dispatch", func() {
		It("rejects unsupported database", func(ctx context.Context) {
			_, err := s.query(ctx, sqlRequest{
				Query:    "SELECT 1",
				DSN:      "test",
				Database: "mongodb",
			})
			Expect(err).To(MatchError(ContainSubstring("unsupported database")))
		})

		It("handles postgres alias", func(ctx context.Context) {
			_, err := s.query(ctx, sqlRequest{
				Query:    "SELECT 1",
				DSN:      "fake://dsn",
				Database: "postgres",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("query failed"))
		})

		It("handles pg alias", func(ctx context.Context) {
			_, err := s.query(ctx, sqlRequest{
				Query:    "SELECT 1",
				DSN:      "fake://dsn",
				Database: "pg",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("query failed"))
		})

		It("handles sqlite3 alias", func(ctx context.Context) {
			_, err := s.query(ctx, sqlRequest{
				Query:    "SELECT 1",
				DSN:      "/nonexistent.db",
				Database: "sqlite3",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("query failed"))
		})

		It("defaults empty database", func(ctx context.Context) {
			_, err := s.query(ctx, sqlRequest{
				Query:    "SELECT 1",
				DSN:      "fake://dsn",
				Database: "",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("query failed"))
		})
	})

	Describe("provider", func() {
		It("creates tool via provider", func() {
			p := NewToolProvider(fakeSecretProvider)
			tools := p.GetTools("test_db")
			Expect(tools).To(HaveLen(1))
			Expect(tools[0].Declaration().Name).To(Equal("test_db_sql_query"))
		})
	})
})
