// Package qrutil provides utilities for generating QR code images and
// terminal-printable QR codes from arbitrary string content such as URLs,
// pairing codes, or any text that needs to be shared visually.
//
// This package wraps github.com/skip2/go-qrcode and provides convenience
// functions for common operations like saving a QR code as a PNG file and
// rendering a QR code as Unicode block characters for terminal display.
//
// Without this package, each consumer (WhatsApp pairing, sharing links, etc.)
// would need to duplicate QR generation logic and handle error cases independently.
package qrutil

import (
	"fmt"
	"path/filepath"

	qrcode "github.com/skip2/go-qrcode"
)

// defaultSize is the default pixel dimension (width=height) for generated PNG images.
const defaultSize = 256

// GeneratePNG writes a QR code PNG image for the given content to the specified
// directory. The file is named "qr-code.png". Returns the full path to the
// written file, or an error if generation fails.
//
// This function exists so that callers can provide a scannable QR image to end
// users, especially when the terminal does not render Unicode QR codes reliably.
func GeneratePNG(content string, dir string) (string, error) {
	path := filepath.Join(dir, "qr-code.png")
	if err := qrcode.WriteFile(content, qrcode.Medium, defaultSize, path); err != nil {
		return "", fmt.Errorf("qrutil: failed to write QR code image: %w", err)
	}
	return path, nil
}

// ToTerminalString returns a compact Unicode block-character representation of
// the QR code suitable for printing to a terminal. The result uses half-block
// characters (▀▄█) that render as a scannable QR code in most modern terminal
// emulators. Returns the raw content string as a fallback if QR generation fails.
//
// This function exists so that QR codes can be displayed inline in the terminal
// without requiring the user to open a separate image file.
func ToTerminalString(content string) string {
	qr, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return content
	}
	return qr.ToSmallString(false)
}

// PrintToTerminal prints a QR code to stdout with a header box, the Unicode QR
// image, the saved file path, and an optional instruction message.
//
// Parameters:
//   - content: the string to encode as a QR code
//   - dir: directory where the PNG image will be saved
//   - header: text displayed in the box above the QR code (e.g. "Scan this QR code with WhatsApp")
//   - instruction: text displayed below the QR code (e.g. "Open WhatsApp → Settings → ...")
//
// This function exists to provide a consistent, polished QR code presentation
// across different features that need to show QR codes to the user.
func PrintToTerminal(content string, dir string, header string, instruction string) (string, error) {
	pngPath, err := GeneratePNG(content, dir)
	if err != nil {
		// Non-fatal: still print the terminal QR.
		pngPath = ""
	}

	fmt.Printf("\n╔══════════════════════════════════════════╗\n")
	fmt.Printf("║   %-38s ║\n", header)
	fmt.Printf("╚══════════════════════════════════════════╝\n")
	fmt.Println(ToTerminalString(content))

	if pngPath != "" {
		fmt.Printf("  QR code image saved to: %s\n", pngPath)
	}
	if instruction != "" {
		fmt.Printf("  %s\n", instruction)
	}
	fmt.Println()

	return pngPath, err
}
