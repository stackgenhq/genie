// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package skills

//counterfeiter:generate . ISkillWriter

// ISkillWriter is the subset of MutableRepository that consumers need for
// writing skills. Using this interface (rather than the concrete
// *MutableRepository) makes callers easy to unit-test with fakes.
type ISkillWriter interface {
	// Add creates a new skill on disk and updates the in-memory index.
	Add(req AddSkillRequest) error

	// Update overwrites an existing skill's content on disk.
	Update(req AddSkillRequest) error

	// Exists returns true if a skill with the given name already exists.
	Exists(name string) bool
}
