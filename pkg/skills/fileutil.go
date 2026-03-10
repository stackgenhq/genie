// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"fmt"
	"io"
	"os"
)

// readFileWithLimit reads a file with a size limit.
// This function exists to prevent reading extremely large files into memory.
// Without this function, large output files could cause out-of-memory errors.
func readFileWithLimit(path string, maxSize int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	// Get file size
	info, err := file.Stat()
	if err != nil {
		return "", err
	}

	// Check size limit
	if maxSize > 0 && info.Size() > maxSize {
		return "", fmt.Errorf("file exceeds size limit (%d bytes > %d bytes)",
			info.Size(), maxSize)
	}

	// Read with limit
	var limitReader io.Reader = file
	if maxSize > 0 {
		limitReader = io.LimitReader(file, maxSize)
	}

	content, err := io.ReadAll(limitReader)
	if err != nil {
		return "", err
	}

	return string(content), nil
}
