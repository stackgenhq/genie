// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package osutils

import (
	"os"
	"path/filepath"
	"strings"
)

// Getwd returns the current working directory as an absolute path.
// Unlike os.Getwd, this returns a clean error instead of panicking
// when the directory cannot be determined.
func Getwd() (string, error) {
	return os.Getwd()
}

func FindFileCaseInsensitive(dir, name string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	target := strings.ToLower(name)
	for _, entry := range entries {
		if strings.ToLower(entry.Name()) == target {
			return filepath.Join(dir, entry.Name()), nil
		}
	}

	return "", os.ErrNotExist
}

func GetAllFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
