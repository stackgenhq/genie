package auth

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/stackgenhq/genie/pkg/security/keyring"
)

const (
	// aguiPasswordEnv is the environment variable checked for the password.
	aguiPasswordEnv = "AGUI_PASSWORD"

	// generatedPasswordBytes is the number of random bytes for auto-generated passwords (32 bytes = 64 hex chars).
	generatedPasswordBytes = 16
)

// resolvePassword determines the AG-UI password from the first available source:
//  1. Config.Password field (set directly in genie.toml / struct)
//  2. AGUI_PASSWORD environment variable
//  3. OS keyring (keyring.AccountAGUIPassword)
//  4. Auto-generated random password (printed to stdout so the operator can find it)
//
// Returns the resolved password as bytes. Never returns nil when called
// (the caller already checked that password protection is enabled).
func resolvePassword(cfg Config) []byte {
	// 1. Explicit config value.
	if cfg.Password != "" {
		return []byte(cfg.Password)
	}

	// 2. Environment variable.
	if env := os.Getenv(aguiPasswordEnv); env != "" {
		return []byte(env)
	}

	// 3. OS keyring (works on desktop, may fail in containers).
	if val, err := keyring.KeyringGet(keyring.AccountAGUIPassword); err == nil && len(val) > 0 {
		return val
	}

	// 4. Auto-generate and print.
	return generateAndPrintPassword()
}

// generateAndPrintPassword creates a cryptographically random password,
// prints it to stdout so the operator can retrieve it from container logs,
// and returns it. This is the last-resort for cloud deployments where no
// password was explicitly configured.
func generateAndPrintPassword() []byte {
	buf := make([]byte, generatedPasswordBytes)
	if _, err := cryptorand.Read(buf); err != nil {
		// Extremely unlikely; crypto/rand uses the OS CSPRNG.
		panic(fmt.Sprintf("auth: failed to generate random password: %v", err))
	}
	password := hex.EncodeToString(buf)

	// Print prominently so the operator can find it in logs.
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  AG-UI Auto-Generated Password (password_protected=true)   ║")
	fmt.Printf("║  Password: %-48s ║\n", password)
	fmt.Println("║                                                            ║")
	fmt.Println("║  Set AGUI_PASSWORD env var or config to use a fixed value. ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	return []byte(password)
}
