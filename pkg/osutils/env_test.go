// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package osutils_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/osutils"
)

var _ = Describe("Getenv", func() {
	It("should return env var value when set", func() {
		os.Setenv("OSUTILS_TEST_KEY", "myvalue")
		defer os.Unsetenv("OSUTILS_TEST_KEY")

		result := osutils.Getenv("OSUTILS_TEST_KEY", "default")
		Expect(result).To(Equal("myvalue"))
	})

	It("should return default when env var is not set", func() {
		os.Unsetenv("OSUTILS_NONEXISTENT_KEY")
		result := osutils.Getenv("OSUTILS_NONEXISTENT_KEY", "fallback")
		Expect(result).To(Equal("fallback"))
	})

	It("should return empty string when env var is set to empty", func() {
		os.Setenv("OSUTILS_EMPTY_KEY", "")
		defer os.Unsetenv("OSUTILS_EMPTY_KEY")

		result := osutils.Getenv("OSUTILS_EMPTY_KEY", "fallback")
		Expect(result).To(Equal(""))
	})
})

var _ = Describe("Getwd", func() {
	It("should return the current working directory", func() {
		dir, err := osutils.Getwd()
		Expect(err).NotTo(HaveOccurred())
		Expect(dir).NotTo(BeEmpty())
		Expect(filepath.IsAbs(dir)).To(BeTrue())
	})
})

var _ = Describe("GetAllFiles", func() {
	It("should list all files in a directory", func() {
		tmpDir := GinkgoT().TempDir()
		err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0644)
		Expect(err).NotTo(HaveOccurred())

		files, err := osutils.GetAllFiles(tmpDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveLen(2))
	})

	It("should recurse into subdirectories", func() {
		tmpDir := GinkgoT().TempDir()
		subDir := filepath.Join(tmpDir, "sub")
		err := os.MkdirAll(subDir, 0755)
		Expect(err).NotTo(HaveOccurred())

		err = os.WriteFile(filepath.Join(tmpDir, "root.txt"), []byte("r"), 0644)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("n"), 0644)
		Expect(err).NotTo(HaveOccurred())

		files, err := osutils.GetAllFiles(tmpDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveLen(2))
	})

	It("should return empty for an empty directory", func() {
		tmpDir := GinkgoT().TempDir()
		files, err := osutils.GetAllFiles(tmpDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(BeEmpty())
	})

	It("should return error for non-existent directory", func() {
		_, err := osutils.GetAllFiles("/nonexistent/path")
		Expect(err).To(HaveOccurred())
	})
})
