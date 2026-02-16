package osutils

import (
	"os"
	"path/filepath"
	"strings"
)

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
