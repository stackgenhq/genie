// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package datasource_test

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/datasource"
)

var _ = Describe("SyncState", func() {
	var dir string

	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "genie-syncstate-")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	})

	Describe("LoadSyncState", func() {
		It("returns nil when file does not exist", func() {
			state, err := datasource.LoadSyncState(dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(state).To(BeNil())
		})

		It("returns parsed state when file exists", func() {
			path := filepath.Join(dir, "datasource_sync_state.json")
			Expect(os.WriteFile(path, []byte(`{"gmail":"2025-01-15T10:00:00Z","gdrive":"2025-01-16T12:00:00Z"}`), 0o600)).To(Succeed())
			state, err := datasource.LoadSyncState(dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(state).NotTo(BeNil())
			Expect(state.LastSync("gmail").Year()).To(Equal(2025))
			Expect(state.LastSync("gdrive").Year()).To(Equal(2025))
		})
	})

	Describe("SaveSyncState and LoadSyncState roundtrip", func() {
		It("persists and restores state", func() {
			state := make(datasource.SyncState)
			state.SetLastSync("gmail", mustParseRFC3339("2025-02-01T00:00:00Z"))
			Expect(datasource.SaveSyncState(dir, state)).To(Succeed())

			loaded, err := datasource.LoadSyncState(dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded).NotTo(BeNil())
			t := loaded.LastSync("gmail")
			Expect(t.UTC().Format("2006-01-02")).To(Equal("2025-02-01"))
		})
	})

	Describe("LastSync and SetLastSync", func() {
		It("returns zero time for missing or nil state", func() {
			var s datasource.SyncState
			Expect(s.LastSync("gmail").IsZero()).To(BeTrue())
			s = make(datasource.SyncState)
			Expect(s.LastSync("gmail").IsZero()).To(BeTrue())
		})

		It("returns parsed time after SetLastSync", func() {
			s := make(datasource.SyncState)
			s.SetLastSync("gdrive", mustParseRFC3339("2025-03-01T12:00:00Z"))
			Expect(s.LastSync("gdrive").UTC().Format(time.RFC3339)).To(Equal("2025-03-01T12:00:00Z"))
		})
	})
})

func mustParseRFC3339(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
