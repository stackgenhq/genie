// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Config", func() {
	DescribeTable("isPostgres",
		func(cfg Config, expected bool) {
			Expect(cfg.isPostgres()).To(Equal(expected))
		},
		Entry("default config is SQLite",
			DefaultConfig(), false),
		Entry("DSN set means Postgres",
			Config{DSN: "postgres://user:pass@localhost:5432/genie"}, true),
		Entry("both set prefers Postgres",
			Config{DBFile: "/tmp/test.db", DSN: "postgres://user:pass@localhost:5432/genie"}, true),
		Entry("only DBFile is SQLite",
			Config{DBFile: "/tmp/test.db"}, false),
	)

	DescribeTable("DisplayPath",
		func(cfg Config, expected string) {
			Expect(cfg.DisplayPath()).To(Equal(expected))
		},
		Entry("SQLite shows file path",
			Config{DBFile: "/data/genie.db"}, "/data/genie.db"),
		Entry("Postgres masks password",
			Config{DSN: "postgres://admin:supersecret@db.example.com:5432/genie?sslmode=disable"},
			"postgres"),
		Entry("Postgres without password",
			Config{DSN: "postgres://admin@db.example.com:5432/genie"},
			"postgres"),
	)
})

var _ = Describe("OpenConfig", func() {
	It("opens a SQLite database", func() {
		dir := GinkgoT().TempDir()
		dbPath := filepath.Join(dir, "test.db")

		gormDB, err := OpenConfig(Config{DBFile: dbPath})
		Expect(err).NotTo(HaveOccurred())

		Expect(AutoMigrate(gormDB)).To(Succeed())
		Expect(Close(gormDB)).To(Succeed())

		_, statErr := os.Stat(dbPath)
		Expect(statErr).NotTo(HaveOccurred(), "database file should exist on disk")
	})

	It("opens a SQLite database with empty DBFile (uses default path)", func() {
		// OpenConfig with empty config should fall through to openSQLite
		// which defaults the path. We test that it doesn't error.
		gormDB, err := OpenConfig(Config{})
		Expect(err).NotTo(HaveOccurred())
		Expect(gormDB).NotTo(BeNil())
		Expect(Close(gormDB)).To(Succeed())
	})

	It("dispatches to openPostgres when DSN is set", func() {
		// This will fail to connect since there's no real Postgres, but
		// it exercises the isPostgres() branch and the openPostgres path.
		_, err := OpenConfig(Config{DSN: "postgres://fakeuser:fakepass@localhost:59999/fakedb?sslmode=disable&connect_timeout=1"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("PostgreSQL"))
	})
})

var _ = Describe("DefaultConfig", func() {
	It("returns a config with a non-empty DBFile", func() {
		cfg := DefaultConfig()
		Expect(cfg.DBFile).NotTo(BeEmpty())
		Expect(cfg.DSN).To(BeEmpty())
	})

	It("DisplayPath returns the DBFile for default config", func() {
		cfg := DefaultConfig()
		Expect(cfg.DisplayPath()).To(Equal(cfg.DBFile))
	})

	It("DisplayPath returns the default path when both are empty", func() {
		cfg := Config{}
		dp := cfg.DisplayPath()
		Expect(dp).To(ContainSubstring("genie.db"))
	})
})

var _ = Describe("Open (backward compat)", func() {
	It("opens a SQLite database via the deprecated Open helper", func() {
		dir := GinkgoT().TempDir()
		dbPath := filepath.Join(dir, "compat.db")

		gormDB, err := Open(dbPath)
		Expect(err).NotTo(HaveOccurred())

		Expect(AutoMigrate(gormDB)).To(Succeed())
		Expect(Close(gormDB)).To(Succeed())
	})
})
