package osutils

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Platform detection constants, modelled after internal/goos.
var (
	isDarwin = runtime.GOOS == "darwin"
	isLinux  = runtime.GOOS == "linux"
)

// Package manager keys for use in osPackageNames maps.
const (
	Brew   = "brew"
	Apt    = "apt"
	Dnf    = "dnf"
	Pacman = "pacman"
	Apk    = "apk"
)

// InstallHint returns a platform-appropriate installation command.
// osPackageNames maps package manager names to their package names.
// If a manager-specific name is missing, packageName is used as fallback.
//
// Example:
//
//	InstallHint("tesseract", map[string]string{"apt": "tesseract-ocr"})
//	// macOS  → "brew install tesseract"
//	// Linux  → "apt-get install tesseract-ocr (Debian/Ubuntu) or ..."
func InstallHint(packageName string, osPackageNames map[string]string) string {
	pkg := func(mgr string) string {
		if name, ok := osPackageNames[mgr]; ok {
			return name
		}
		return packageName
	}

	switch {
	case isDarwin:
		return "brew install " + pkg(Brew)
	case isLinux:
		return "apt-get install " + pkg(Apt) +
			" (Debian/Ubuntu) or dnf install " + pkg(Dnf) +
			" (Fedora/RHEL) or pacman -S " + pkg(Pacman) +
			" (Arch) or apk add " + pkg(Apk) + " (Alpine)"
	default:
		return "install " + packageName + " for your platform"
	}
}

// ToolNotFoundError returns an error recommending installation of a missing CLI tool.
//
// Example:
//
//	ToolNotFoundError("tesseract", map[string]string{"apt": "tesseract-ocr"})
//	// → error("tesseract not found: install with: brew install tesseract")
func ToolNotFoundError(toolName string, osPackageNames map[string]string) error {
	return fmt.Errorf("%s not found: install with: %s", toolName, InstallHint(toolName, osPackageNames))
}

// ValidateToolAvailability checks if a tool is available on the system.
// If not, it returns an error recommending installation.
func ValidateToolAvailability(toolName string, osPackageNames map[string]string) error {
	if _, err := exec.LookPath(toolName); err != nil {
		return ToolNotFoundError(toolName, osPackageNames)
	}
	return nil
}
