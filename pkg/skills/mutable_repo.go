// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/skill"
)

const (
	// skillFile is the canonical skill definition filename.
	skillFile = "SKILL.md"

	// maxSkillNameLen is the maximum length of a skill name.
	maxSkillNameLen = 64

	// defaultMaxSkills is the default maximum number of skills that can be stored.
	defaultMaxSkills = 50

	// dirPerm is the permission for created directories.
	dirPerm = 0o755

	// scriptPerm is the permission for executable script files.
	scriptPerm = 0o755

	// docPerm is the permission for document files (non-executable).
	docPerm = 0o644
)

// skillNamePattern validates skill names: lowercase alphanumeric, hyphens, underscores.
var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9\-_]*$`)

// AddSkillRequest contains all parameters needed to create a new skill.
type AddSkillRequest struct {
	// Name is the skill identifier (lowercase alphanumeric + hyphens/underscores).
	Name string

	// Description is a one-line summary of the skill.
	Description string

	// Instructions is the markdown body of the SKILL.md file.
	Instructions string

	// Scripts maps filename to plain-text content (Python, bash, etc.).
	// These are written with executable permissions.
	Scripts map[string]string

	// Docs maps filename to base64-encoded binary content (PDF, docx, xlsx, pptx, etc.).
	// Content is decoded before writing. For plain text docs, use raw text (not base64).
	Docs map[string]string
}

// MutableRepository implements skill.Repository backed by a writable directory.
// Unlike FSRepository (which scans once at startup), this repository maintains
// an in-memory index that is updated when Add() is called, making new skills
// immediately discoverable without a pod restart.
type MutableRepository struct {
	mu        sync.RWMutex
	baseDir   string
	maxSkills int
	// name -> directory path containing SKILL.md
	index map[string]string
}

// NewMutableRepository creates a MutableRepository rooted at baseDir.
// It scans the directory for any existing skills (from prior runs).
func NewMutableRepository(baseDir string, maxSkills int) (*MutableRepository, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("mutable repository: base directory is required")
	}
	if maxSkills <= 0 {
		maxSkills = defaultMaxSkills
	}

	if err := os.MkdirAll(baseDir, dirPerm); err != nil {
		return nil, fmt.Errorf("mutable repository: create base dir: %w", err)
	}

	r := &MutableRepository{
		baseDir:   baseDir,
		maxSkills: maxSkills,
		index:     make(map[string]string),
	}

	// Scan for any skills that already exist (from previous pod runs or manual creation).
	r.scanExisting()

	return r, nil
}

// Add creates a new skill on disk and updates the in-memory index.
func (r *MutableRepository) Add(req AddSkillRequest) error {
	if err := r.validateName(req.Name); err != nil {
		return err
	}
	if err := validateContent(req); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.index[req.Name]; exists {
		return fmt.Errorf("create skill: skill %q already exists", req.Name)
	}
	if len(r.index) >= r.maxSkills {
		return fmt.Errorf("create skill: maximum number of skills (%d) reached", r.maxSkills)
	}

	skillDir := filepath.Join(r.baseDir, req.Name)
	if err := r.writeSkillToDisk(skillDir, req); err != nil {
		return err
	}

	r.index[req.Name] = skillDir
	return nil
}

// Update overwrites an existing skill's content on disk.
// The old scripts/ and docs/ directories are removed and replaced.
func (r *MutableRepository) Update(req AddSkillRequest) error {
	if err := r.validateName(req.Name); err != nil {
		return err
	}
	if err := validateContent(req); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	skillDir, exists := r.index[req.Name]
	if !exists {
		return fmt.Errorf("update skill: skill %q does not exist", req.Name)
	}

	// Remove old content then rewrite
	_ = os.RemoveAll(skillDir)
	if err := r.writeSkillToDisk(skillDir, req); err != nil {
		// Attempt to remove partially-written directory
		_ = os.RemoveAll(skillDir)
		delete(r.index, req.Name)
		return err
	}

	return nil
}

// Delete removes a skill from disk and the in-memory index.
func (r *MutableRepository) Delete(name string) error {
	if name == "" {
		return fmt.Errorf("delete skill: name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	skillDir, exists := r.index[name]
	if !exists {
		return fmt.Errorf("delete skill: skill %q does not exist", name)
	}

	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("delete skill: remove directory: %w", err)
	}

	delete(r.index, name)
	return nil
}

// validateContent checks that description and instructions are non-empty.
func validateContent(req AddSkillRequest) error {
	if strings.TrimSpace(req.Description) == "" {
		return fmt.Errorf("create skill: description is required")
	}
	if strings.TrimSpace(req.Instructions) == "" {
		return fmt.Errorf("create skill: instructions are required")
	}
	return nil
}

// writeSkillToDisk writes a skill's SKILL.md, scripts, and docs to the given directory.
func (r *MutableRepository) writeSkillToDisk(skillDir string, req AddSkillRequest) error {
	if err := os.MkdirAll(skillDir, dirPerm); err != nil {
		return fmt.Errorf("create skill: mkdir: %w", err)
	}

	// Write SKILL.md with YAML frontmatter
	skillContent := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s\n",
		req.Name, req.Description, req.Instructions)
	if err := os.WriteFile(filepath.Join(skillDir, skillFile), []byte(skillContent), docPerm); err != nil {
		_ = os.RemoveAll(skillDir)
		return fmt.Errorf("create skill: write SKILL.md: %w", err)
	}

	// Write scripts (executable permissions)
	if len(req.Scripts) > 0 {
		scriptsDir := filepath.Join(skillDir, "scripts")
		if err := os.MkdirAll(scriptsDir, dirPerm); err != nil {
			_ = os.RemoveAll(skillDir)
			return fmt.Errorf("create skill: mkdir scripts: %w", err)
		}
		for name, content := range req.Scripts {
			if err := validateFilename(name); err != nil {
				_ = os.RemoveAll(skillDir)
				return fmt.Errorf("create skill: script %q: %w", name, err)
			}
			if err := os.WriteFile(filepath.Join(scriptsDir, name), []byte(content), scriptPerm); err != nil {
				_ = os.RemoveAll(skillDir)
				return fmt.Errorf("create skill: write script %q: %w", name, err)
			}
		}
	}

	// Write docs (base64-decoded binary content)
	if len(req.Docs) > 0 {
		docsDir := filepath.Join(skillDir, "docs")
		if err := os.MkdirAll(docsDir, dirPerm); err != nil {
			_ = os.RemoveAll(skillDir)
			return fmt.Errorf("create skill: mkdir docs: %w", err)
		}
		for name, b64Content := range req.Docs {
			if err := validateFilename(name); err != nil {
				_ = os.RemoveAll(skillDir)
				return fmt.Errorf("create skill: doc %q: %w", name, err)
			}
			decoded, err := base64.StdEncoding.DecodeString(b64Content)
			if err != nil {
				_ = os.RemoveAll(skillDir)
				return fmt.Errorf("create skill: decode doc %q: %w", name, err)
			}
			if err := os.WriteFile(filepath.Join(docsDir, name), decoded, docPerm); err != nil {
				_ = os.RemoveAll(skillDir)
				return fmt.Errorf("create skill: write doc %q: %w", name, err)
			}
		}
	}

	return nil
}

// Summaries returns all available skill summaries.
func (r *MutableRepository) Summaries() []skill.Summary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	summaries := make([]skill.Summary, 0, len(r.index))
	for name, dir := range r.index {
		desc := r.readDescription(dir)
		summaries = append(summaries, skill.Summary{
			Name:        name,
			Description: desc,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})

	return summaries
}

// Get returns a full skill by name.
func (r *MutableRepository) Get(name string) (*skill.Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dir, ok := r.index[name]
	if !ok {
		return nil, fmt.Errorf("skill %q not found", name)
	}

	skillPath := filepath.Join(dir, skillFile)
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("read skill %q: %w", name, err)
	}

	summary, body := parseFrontMatter(string(data))
	if summary.Name == "" {
		summary.Name = name
	}

	// Collect doc files (*.md, *.txt in skill dir and docs/ subdir)
	docs := r.collectDocs(dir)

	return &skill.Skill{
		Summary: summary,
		Body:    body,
		Docs:    docs,
	}, nil
}

// Path returns the directory path that contains the given skill.
func (r *MutableRepository) Path(name string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dir, ok := r.index[name]
	if !ok {
		return "", fmt.Errorf("skill %q not found", name)
	}
	return dir, nil
}

// Count returns the number of skills in the repository.
func (r *MutableRepository) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.index)
}

// scanExisting walks baseDir and indexes any existing skill directories.
func (r *MutableRepository) scanExisting() {
	entries, err := os.ReadDir(r.baseDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(r.baseDir, entry.Name())
		sf := filepath.Join(dir, skillFile)
		if _, err := os.Stat(sf); err == nil {
			name := entry.Name()
			// Try to read name from frontmatter
			if data, readErr := os.ReadFile(sf); readErr == nil {
				summary, _ := parseFrontMatter(string(data))
				if summary.Name != "" {
					name = summary.Name
				}
			}
			r.index[name] = dir
		}
	}
}

// readDescription reads the description from a skill's SKILL.md frontmatter.
func (r *MutableRepository) readDescription(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, skillFile))
	if err != nil {
		return ""
	}
	summary, _ := parseFrontMatter(string(data))
	return summary.Description
}

// collectDocs gathers *.md and *.txt files from the skill directory (excluding SKILL.md).
func (r *MutableRepository) collectDocs(dir string) []skill.Doc {
	var docs []skill.Doc
	// Walk the skill directory for text-based doc files
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		base := filepath.Base(p)
		if base == skillFile {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(base))
		if ext != ".md" && ext != ".txt" {
			return nil
		}
		content, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		docs = append(docs, skill.Doc{
			Path:    rel,
			Content: string(content),
		})
		return nil
	})
	return docs
}

// parseFrontMatter extracts YAML frontmatter from a SKILL.md file.
// Returns the summary and the body (everything after the frontmatter).
func parseFrontMatter(content string) (skill.Summary, string) {
	var summary skill.Summary
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return summary, content
	}

	// Find the closing ---
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return summary, content
	}

	frontMatter := rest[:idx]
	body := strings.TrimSpace(rest[idx+4:])

	// Simple key: value parsing (avoids pulling in yaml dependency)
	for _, line := range strings.Split(frontMatter, "\n") {
		line = strings.TrimSpace(line)
		if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			switch key {
			case "name":
				summary.Name = val
			case "description":
				summary.Description = val
			}
		}
	}

	return summary, body
}

// validateName checks that a skill name is valid.
func (r *MutableRepository) validateName(name string) error {
	if name == "" {
		return fmt.Errorf("create skill: name is required")
	}
	if len(name) > maxSkillNameLen {
		return fmt.Errorf("create skill: name exceeds maximum length of %d characters", maxSkillNameLen)
	}
	if !skillNamePattern.MatchString(name) {
		return fmt.Errorf("create skill: name %q is invalid (must be lowercase alphanumeric with hyphens/underscores)", name)
	}
	return nil
}

// validateFilename checks that a filename is safe (no path traversal, no absolute paths).
func validateFilename(name string) error {
	if name == "" {
		return fmt.Errorf("filename is required")
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("absolute paths not allowed")
	}
	cleaned := filepath.Clean(name)
	if strings.Contains(cleaned, "..") || strings.Contains(cleaned, string(filepath.Separator)) {
		return fmt.Errorf("path traversal not allowed")
	}
	return nil
}
