package osutils_test

import (
	"os"
	"path/filepath"

	"github.com/appcd-dev/genie/pkg/osutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("File Operations", func() {
	Context("FindFileCaseInsensitive", func() {
		var tempDir string

		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "osutils-test")
			Expect(err).NotTo(HaveOccurred())

			// Create some test files
			Expect(os.WriteFile(filepath.Join(tempDir, "TestFile.txt"), []byte("content"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "another.TXT"), []byte("content"), 0644)).To(Succeed())
		})

		AfterEach(func() {
			os.RemoveAll(tempDir)
		})

		It("should find a file with exact case match", func() {
			path, err := osutils.FindFileCaseInsensitive(tempDir, "TestFile.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal(filepath.Join(tempDir, "TestFile.txt")))
		})

		It("should find a file with different case", func() {
			path, err := osutils.FindFileCaseInsensitive(tempDir, "testfile.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal(filepath.Join(tempDir, "TestFile.txt")))

			path, err = osutils.FindFileCaseInsensitive(tempDir, "ANOTHER.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal(filepath.Join(tempDir, "another.TXT")))
		})

		It("should return error if file does not exist", func() {
			_, err := osutils.FindFileCaseInsensitive(tempDir, "nonexistent.txt")
			Expect(err).To(MatchError(os.ErrNotExist))
		})

		It("should return error if directory does not exist", func() {
			_, err := osutils.FindFileCaseInsensitive("/path/to/nonexistent/dir", "file.txt")
			Expect(err).To(HaveOccurred())
		})
	})
})
