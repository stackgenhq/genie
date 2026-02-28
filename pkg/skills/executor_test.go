package skills_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/skills"
)

var _ = Describe("LocalExecutor", func() {
	var (
		executor    skills.Executor
		baseWorkDir string
		skillPath   string
	)

	BeforeEach(func() {
		// Create temporary work directory
		baseWorkDir = GinkgoT().TempDir()
		executor = skills.NewLocalExecutor(baseWorkDir)

		// Get path to test skill
		cwd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		skillPath = filepath.Join(cwd, "testdata", "skills", "example-skill")
	})

	Describe("Execute", func() {
		Context("with valid Python script", func() {
			It("should execute script successfully", func() {
				// Create input file content
				inputFiles := map[string]string{
					"input.txt": "Hello World",
				}

				req := skills.ExecuteRequest{
					SkillPath:  skillPath,
					ScriptPath: "scripts/process.py",
					Args: []string{
						"input/input.txt",
						"output/output.txt",
					},
					InputFiles: inputFiles,
					Timeout:    5 * time.Second,
				}

				resp, err := executor.Execute(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.ExitCode).To(Equal(0))
				Expect(resp.Output).To(ContainSubstring("Processed"))
				Expect(resp.OutputFiles).To(HaveKey("output.txt"))
				Expect(resp.OutputFiles["output.txt"]).To(Equal("Hello World"))
			})

			It("should execute script with uppercase option", func() {
				inputFiles := map[string]string{
					"input.txt": "hello world",
				}

				req := skills.ExecuteRequest{
					SkillPath:  skillPath,
					ScriptPath: "scripts/process.py",
					Args: []string{
						"input/input.txt",
						"output/output.txt",
						"--uppercase",
					},
					InputFiles: inputFiles,
					Timeout:    5 * time.Second,
				}

				resp, err := executor.Execute(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.ExitCode).To(Equal(0))
				Expect(resp.OutputFiles).To(HaveKey("output.txt"))
				Expect(resp.OutputFiles["output.txt"]).To(Equal("HELLO WORLD"))
			})

			It("should execute script with word count option", func() {
				inputFiles := map[string]string{
					"input.txt": "hello world test",
				}

				req := skills.ExecuteRequest{
					SkillPath:  skillPath,
					ScriptPath: "scripts/process.py",
					Args: []string{
						"input/input.txt",
						"output/output.txt",
						"--count-words",
					},
					InputFiles: inputFiles,
					Timeout:    5 * time.Second,
				}

				resp, err := executor.Execute(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.ExitCode).To(Equal(0))
				Expect(resp.OutputFiles).To(HaveKey("output.txt"))
				Expect(resp.OutputFiles["output.txt"]).To(ContainSubstring("Word count: 3"))
			})
		})

		Context("with non-existent script", func() {
			It("should return error", func() {
				req := skills.ExecuteRequest{
					SkillPath:  skillPath,
					ScriptPath: "scripts/nonexistent.py",
					Args:       []string{},
					Timeout:    5 * time.Second,
				}

				resp, err := executor.Execute(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not found"))
				Expect(resp.ExitCode).To(Equal(0))
			})
		})

		Context("with script that exits with error", func() {
			It("should return non-zero exit code", func() {
				// Create a script that exits with error
				tempSkillDir := GinkgoT().TempDir()
				scriptDir := filepath.Join(tempSkillDir, "scripts")
				err := os.MkdirAll(scriptDir, 0755)
				Expect(err).NotTo(HaveOccurred())

				scriptPath := filepath.Join(scriptDir, "error.py")
				scriptContent := "#!/usr/bin/env python3\nimport sys\nsys.exit(1)"
				err = os.WriteFile(scriptPath, []byte(scriptContent), 0755)
				Expect(err).NotTo(HaveOccurred())

				req := skills.ExecuteRequest{
					SkillPath:  tempSkillDir,
					ScriptPath: "scripts/error.py",
					Args:       []string{},
					Timeout:    5 * time.Second,
				}

				resp, err := executor.Execute(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.ExitCode).To(Equal(1))
				Expect(resp.Error).NotTo(BeEmpty())
			})
		})

		Context("with timeout", func() {
			It("should timeout long-running script", func() {
				// Create a script that sleeps
				tempSkillDir := GinkgoT().TempDir()
				scriptDir := filepath.Join(tempSkillDir, "scripts")
				err := os.MkdirAll(scriptDir, 0755)
				Expect(err).NotTo(HaveOccurred())

				scriptPath := filepath.Join(scriptDir, "sleep.py")
				scriptContent := "#!/usr/bin/env python3\nimport time\ntime.sleep(10)"
				err = os.WriteFile(scriptPath, []byte(scriptContent), 0755)
				Expect(err).NotTo(HaveOccurred())

				req := skills.ExecuteRequest{
					SkillPath:  tempSkillDir,
					ScriptPath: "scripts/sleep.py",
					Args:       []string{},
					Timeout:    1 * time.Second,
				}

				ctx := context.Background()
				_, err = executor.Execute(ctx, req)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with environment variables", func() {
			It("should pass environment variables to script", func() {
				// Create a script that uses environment variables
				tempSkillDir := GinkgoT().TempDir()
				scriptDir := filepath.Join(tempSkillDir, "scripts")
				err := os.MkdirAll(scriptDir, 0755)
				Expect(err).NotTo(HaveOccurred())

				scriptPath := filepath.Join(scriptDir, "env.py")
				scriptContent := `#!/usr/bin/env python3
import os
import sys
print(os.environ.get('TEST_VAR', 'not set'))
with open(sys.argv[1], 'w') as f:
    f.write(os.environ.get('TEST_VAR', 'not set'))
`
				err = os.WriteFile(scriptPath, []byte(scriptContent), 0755)
				Expect(err).NotTo(HaveOccurred())

				req := skills.ExecuteRequest{
					SkillPath:  tempSkillDir,
					ScriptPath: "scripts/env.py",
					Args:       []string{"output/result.txt"},
					Environment: map[string]string{
						"TEST_VAR": "test_value",
					},
					Timeout: 5 * time.Second,
				}

				resp, err := executor.Execute(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.ExitCode).To(Equal(0))
				Expect(resp.Output).To(ContainSubstring("test_value"))
				Expect(resp.OutputFiles).To(HaveKey("result.txt"))
				Expect(resp.OutputFiles["result.txt"]).To(Equal("test_value"))
			})
		})

		Context("with multiple input files", func() {
			It("should stage all input files", func() {
				inputFiles := map[string]string{
					"file1.txt": "content1",
					"file2.txt": "content2",
				}

				// Create a script that reads multiple files
				tempSkillDir := GinkgoT().TempDir()
				scriptDir := filepath.Join(tempSkillDir, "scripts")
				err := os.MkdirAll(scriptDir, 0755)
				Expect(err).NotTo(HaveOccurred())

				scriptPath := filepath.Join(scriptDir, "multi.py")
				scriptContent := `#!/usr/bin/env python3
import sys
with open('input/file1.txt', 'r') as f1:
    content1 = f1.read()
with open('input/file2.txt', 'r') as f2:
    content2 = f2.read()
with open(sys.argv[1], 'w') as out:
    out.write(content1 + ' ' + content2)
`
				err = os.WriteFile(scriptPath, []byte(scriptContent), 0755)
				Expect(err).NotTo(HaveOccurred())

				req := skills.ExecuteRequest{
					SkillPath:  tempSkillDir,
					ScriptPath: "scripts/multi.py",
					Args:       []string{"output/combined.txt"},
					InputFiles: inputFiles,
					Timeout:    5 * time.Second,
				}

				resp, err := executor.Execute(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.ExitCode).To(Equal(0))
				Expect(resp.OutputFiles).To(HaveKey("combined.txt"))
				Expect(resp.OutputFiles["combined.txt"]).To(Equal("content1 content2"))
			})
		})
	})
})
