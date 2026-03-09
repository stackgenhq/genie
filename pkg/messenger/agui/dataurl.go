// Package agui — dataurl.go extracts embedded data-URL files from AG-UI chat
// messages sent by the browser client, saves them to a temp directory, and
// returns clean message text plus messenger.Attachment structs with LocalPath
// set so the multimodal pipeline can process them like WhatsApp media.
package agui

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/messenger/media"
)

// dataURLFilePattern matches the [file:name:mime]\ndata:mime;base64,... blocks
// inserted by the chat.js multimedia upload code.
//
// Group 1 = file name
// Group 2 = declared MIME type
// Group 3 = data URL (data:mime;base64,...)
//
// The base64 character class [A-Za-z0-9+/=] intentionally excludes whitespace
// so the match stops at the first newline after the encoded data. The
// decodeDataURL function handles any internal whitespace separately.
var dataURLFilePattern = regexp.MustCompile(
	`\[file:([^:]+):([^\]]+)\]\s*(data:[^;]+;base64,[A-Za-z0-9+/=]+)`,
)

// ExtractDataURLFiles scans the message text for embedded data-URL file blocks,
// decodes them, saves them to tempDir, and returns:
//   - cleanMessage: the message text with file blocks removed
//   - attachments:  slice of Attachment structs with LocalPath set
//
// If there are no data URLs, the original message and nil attachments are returned.
func ExtractDataURLFiles(message, tempDir string) (cleanMessage string, attachments []messenger.Attachment) {
	matches := dataURLFilePattern.FindAllStringSubmatchIndex(message, -1)
	if len(matches) == 0 {
		return message, nil
	}

	// Ensure temp dir is safe and within os.TempDir() to prevent path traversal.
	// If an unsafe path is provided, fallback to a safe default.
	safeTempDir := filepath.Clean(tempDir)
	if !strings.HasPrefix(safeTempDir, filepath.Clean(os.TempDir())) {
		safeTempDir = filepath.Join(os.TempDir(), "genie-agui-media", "tmp-default")
	}

	// Ensure temp dir exists.
	if err := os.MkdirAll(safeTempDir, 0o755); err != nil {
		return message, nil
	}

	// Build the cleaned message by removing matched blocks.
	var clean strings.Builder
	lastEnd := 0

	for _, match := range matches {
		fullStart, fullEnd := match[0], match[1]
		fileName := message[match[2]:match[3]]
		declaredMIME := message[match[4]:match[5]]
		dataURL := message[match[6]:match[7]]

		// Append text between the previous match and this one.
		clean.WriteString(message[lastEnd:fullStart])
		lastEnd = fullEnd

		// Decode the data URL.
		data, mime, err := decodeDataURL(dataURL)
		if err != nil {
			continue // skip malformed data URLs
		}

		// Use declared MIME if the data URL didn't contain one.
		if mime == "" {
			mime = declaredMIME
		}

		// Save to disk with a unique name.
		// Sanitize fileName to prevent path traversal.
		cleanFileName := filepath.Base(filepath.Clean(fileName))
		if cleanFileName == "." || cleanFileName == "/" || cleanFileName == "\\" {
			cleanFileName = "file"
		}
		ext := filepath.Ext(cleanFileName)
		base := strings.TrimSuffix(cleanFileName, ext)
		if ext == "" {
			ext = media.ExtFromMIME(mime)
		}
		uniqueName := fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext)
		localPath := filepath.Join(safeTempDir, uniqueName)

		if err := os.WriteFile(localPath, data, 0o644); err != nil {
			continue
		}

		attachments = append(attachments, messenger.Attachment{
			Name:        fileName,
			ContentType: mime,
			Size:        int64(len(data)),
			LocalPath:   localPath,
		})
	}

	// Append any remaining text after the last match.
	clean.WriteString(message[lastEnd:])

	// Trim leading/trailing whitespace from the cleaned message.
	cleanedMsg := strings.TrimSpace(clean.String())

	return cleanedMsg, attachments
}

// decodeDataURL parses a "data:mime;base64,..." URL and returns the decoded
// bytes and the MIME type.
func decodeDataURL(dataURL string) ([]byte, string, error) {
	dataURL = strings.TrimSpace(dataURL)

	if !strings.HasPrefix(dataURL, "data:") {
		return nil, "", fmt.Errorf("not a data URL")
	}

	// Strip "data:" prefix.
	rest := dataURL[5:]

	// Split on the first comma to separate header from data.
	commaIdx := strings.Index(rest, ",")
	if commaIdx < 0 {
		return nil, "", fmt.Errorf("malformed data URL: no comma separator")
	}

	header := rest[:commaIdx]
	encoded := rest[commaIdx+1:]

	// Extract MIME type from header (e.g. "image/png;base64").
	mime := ""
	if semiIdx := strings.Index(header, ";"); semiIdx >= 0 {
		mime = header[:semiIdx]
	} else {
		mime = header
	}

	// Remove whitespace from encoded data (browsers may add line breaks).
	encoded = strings.ReplaceAll(encoded, "\n", "")
	encoded = strings.ReplaceAll(encoded, "\r", "")
	encoded = strings.ReplaceAll(encoded, " ", "")

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// Try with padding tolerance.
		data, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(encoded, "="))
		if err != nil {
			return nil, "", fmt.Errorf("base64 decode failed: %w", err)
		}
	}

	return data, mime, nil
}
