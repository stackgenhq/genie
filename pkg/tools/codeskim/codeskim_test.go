// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package codeskim

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Code Skim Tool (util_code_skim)", func() {
	var (
		s      *skimTools
		tmpDir string
	)

	BeforeEach(func() {
		s = newSkimTools()
		var err error
		tmpDir, err = os.MkdirTemp("", "codeskim-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	Describe("Go files (AST-based)", func() {
		It("extracts package, imports, types, and function signatures", func() {
			goCode := `// Package example provides sample code.
package example

import (
	"fmt"
	"strings"
)

// Greeter greets people.
type Greeter struct {
	Name string
	Age  int
}

// Greet returns a greeting string.
func (g *Greeter) Greet() string {
	return fmt.Sprintf("Hello, %s! You are %d years old.", g.Name, g.Age)
}

// NewGreeter creates a new Greeter.
func NewGreeter(name string, age int) *Greeter {
	return &Greeter{
		Name: strings.TrimSpace(name),
		Age:  age,
	}
}
`
			path := filepath.Join(tmpDir, "example.go")
			Expect(os.WriteFile(path, []byte(goCode), 0644)).To(Succeed())

			resp, err := s.skim(context.Background(), skimRequest{Path: path})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Language).To(Equal("go"))

			// Should include structural elements.
			Expect(resp.Outline).To(ContainSubstring("package example"))
			Expect(resp.Outline).To(ContainSubstring(`"fmt"`))
			Expect(resp.Outline).To(ContainSubstring("type Greeter struct"))
			Expect(resp.Outline).To(ContainSubstring("Name string"))
			Expect(resp.Outline).To(ContainSubstring("func (g *Greeter) Greet()"))
			Expect(resp.Outline).To(ContainSubstring("func NewGreeter(name string, age int)"))

			// Should NOT include implementation bodies.
			Expect(resp.Outline).NotTo(ContainSubstring("Sprintf"))
			Expect(resp.Outline).NotTo(ContainSubstring("TrimSpace"))
		})

		It("extracts interfaces", func() {
			goCode := `package iface

// Reader reads data.
type Reader interface {
	Read(p []byte) (int, error)
	Close() error
}
`
			path := filepath.Join(tmpDir, "iface.go")
			Expect(os.WriteFile(path, []byte(goCode), 0644)).To(Succeed())

			resp, err := s.skim(context.Background(), skimRequest{Path: path})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Outline).To(ContainSubstring("type Reader interface"))
		})
	})

	Describe("Python files (heuristic)", func() {
		It("extracts class and function definitions", func() {
			pyCode := `# Module docstring
import os
from typing import Optional

class Calculator:
    """A simple calculator."""
    
    def __init__(self, precision: int = 2):
        self.precision = precision
    
    def add(self, a: float, b: float) -> float:
        """Add two numbers."""
        return round(a + b, self.precision)

def standalone_function(x: int) -> int:
    """A standalone function."""
    return x * 2

MAX_VALUE = 100
`
			path := filepath.Join(tmpDir, "calc.py")
			Expect(os.WriteFile(path, []byte(pyCode), 0644)).To(Succeed())

			resp, err := s.skim(context.Background(), skimRequest{Path: path})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Language).To(Equal("python"))
			Expect(resp.Outline).To(ContainSubstring("import os"))
			Expect(resp.Outline).To(ContainSubstring("class Calculator:"))
			Expect(resp.Outline).To(ContainSubstring("def standalone_function"))

			// Should not include implementation bodies.
			Expect(resp.Outline).NotTo(ContainSubstring("return round"))
			Expect(resp.Outline).NotTo(ContainSubstring("return x * 2"))
		})
	})

	Describe("TypeScript files (heuristic)", func() {
		It("extracts exports and declarations", func() {
			tsCode := `import { Request, Response } from 'express';

export interface User {
  id: string;
  name: string;
}

export function createUser(name: string): User {
  return { id: generateId(), name };
}

export class UserService {
  private users: User[] = [];
  
  addUser(user: User): void {
    this.users.push(user);
  }
}

const DEFAULT_LIMIT = 100;
`
			path := filepath.Join(tmpDir, "user.ts")
			Expect(os.WriteFile(path, []byte(tsCode), 0644)).To(Succeed())

			resp, err := s.skim(context.Background(), skimRequest{Path: path})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Language).To(Equal("typescript"))
			Expect(resp.Outline).To(ContainSubstring("import"))
			Expect(resp.Outline).To(ContainSubstring("export interface User"))
			Expect(resp.Outline).To(ContainSubstring("export function createUser"))
			Expect(resp.Outline).To(ContainSubstring("export class UserService"))
		})
	})

	Describe("directory skimming", func() {
		It("processes all source files in a directory", func() {
			// Create a Go file.
			goCode := `package main

func main() {
	fmt.Println("hello")
}
`
			Expect(os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(goCode), 0644)).To(Succeed())

			// Create a Python file.
			pyCode := `def hello():
    print("hello")
`
			Expect(os.WriteFile(filepath.Join(tmpDir, "script.py"), []byte(pyCode), 0644)).To(Succeed())

			// Create a non-source file (should be skipped).
			Expect(os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("not code"), 0644)).To(Succeed())

			resp, err := s.skim(context.Background(), skimRequest{Path: tmpDir})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Outline).To(ContainSubstring("main.go"))
			Expect(resp.Outline).To(ContainSubstring("script.py"))
			Expect(resp.ItemCount).To(BeNumerically(">", 0))
		})
	})

	Describe("error cases", func() {
		It("returns error for empty path", func() {
			_, err := s.skim(context.Background(), skimRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("path is required"))
		})

		It("returns error for non-existent path", func() {
			_, err := s.skim(context.Background(), skimRequest{Path: "/nonexistent/file.go"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot access"))
		})
	})
})

var _ = Describe("Language detection", func() {
	var s *skimTools

	BeforeEach(func() {
		s = newSkimTools()
	})

	DescribeTable("detects language from extension",
		func(ext, expected string) {
			Expect(s.languageFromExt(ext)).To(Equal(expected))
		},
		Entry("Go", ".go", "go"),
		Entry("Python", ".py", "python"),
		Entry("JavaScript", ".js", "javascript"),
		Entry("TypeScript", ".ts", "typescript"),
		Entry("Java", ".java", "java"),
		Entry("Rust", ".rs", "rust"),
		Entry("Ruby", ".rb", "ruby"),
		Entry("C", ".c", "c"),
		Entry("C++", ".cpp", "cpp"),
		Entry("C#", ".cs", "csharp"),
		Entry("unknown", ".xyz", "unknown"),
	)
})

var _ = Describe("Code Skim ToolProvider", func() {
	It("returns the expected tool", func() {
		p := NewToolProvider()
		tools := p.GetTools(context.Background())
		Expect(tools).To(HaveLen(1))
		Expect(tools[0].Declaration().Name).To(Equal("code_skim"))
	})
})
