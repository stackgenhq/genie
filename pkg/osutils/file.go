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
