// Package media provides shared utility functions for building and describing
// messenger.Attachment values across platform adapters.
package media

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/appcd-dev/genie/pkg/messenger"
)

// DescribeAttachments generates a human-readable text description of the given
// attachments. This is used to augment user messages so the LLM knows about
// any attached files. Includes local file paths when available so the LLM can
// use read_file to access the content.
//
// When baseDir is non-empty, absolute LocalPaths that fall under baseDir are
// converted to relative paths so the file tool (which rejects absolute paths)
// can resolve them. Paths outside baseDir are left absolute as a fallback.
//
// Example output: "[Attached: report.pdf (application/pdf, 1.2 MB) → .genie/whatsapp/media/report.pdf]"
func DescribeAttachments(attachments []messenger.Attachment, baseDir ...string) string {
	if len(attachments) == 0 {
		return ""
	}

	bd := ""
	if len(baseDir) > 0 {
		bd = baseDir[0]
	}

	var parts []string
	for _, a := range attachments {
		desc := a.Name
		if desc == "" {
			desc = "unnamed file"
		}

		var meta []string
		if a.ContentType != "" {
			meta = append(meta, a.ContentType)
		}
		if a.Size > 0 {
			meta = append(meta, FormatFileSize(a.Size))
		}

		if len(meta) > 0 {
			desc += " (" + strings.Join(meta, ", ") + ")"
		}
		if a.LocalPath != "" {
			desc += " → " + relativizePath(a.LocalPath, bd)
		} else if a.URL != "" {
			desc += " → " + a.URL
		}
		parts = append(parts, desc)
	}

	if len(parts) == 1 {
		return "[Attached: " + parts[0] + "]"
	}
	return "[Attached: " + strings.Join(parts, "; ") + "]"
}

// relativizePath converts an absolute path to a relative path under baseDir.
// If baseDir is empty or the path doesn't start with baseDir, it returns the
// original path unchanged.
func relativizePath(p, baseDir string) string {
	if baseDir == "" || !filepath.IsAbs(p) {
		return p
	}
	rel, err := filepath.Rel(baseDir, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return rel
}

// FormatFileSize returns a human-readable file size string.
func FormatFileSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// mimeTypes maps common file extensions to MIME types. Used as a fallback when
// the platform doesn't provide a MIME type.
var mimeTypes = map[string]string{
	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".csv":  "text/csv",
	".txt":  "text/plain",
	".json": "application/json",
	".xml":  "application/xml",
	".zip":  "application/zip",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".mp4":  "video/mp4",
	".mp3":  "audio/mpeg",
	".ogg":  "audio/ogg",
	".wav":  "audio/wav",
	".webm": "video/webm",
}

// MIMEFromFilename returns a MIME type based on the file extension.
// Returns "application/octet-stream" if the extension is unknown.
func MIMEFromFilename(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if mime, ok := mimeTypes[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}

// NameFromMIME generates a filename from a MIME type and a prefix.
// Used when the platform doesn't provide a filename (e.g., images, audio).
// Example: NameFromMIME("image/jpeg", "image") → "image.jpg"
func NameFromMIME(mime, prefix string) string {
	ext := ExtFromMIME(mime)
	if prefix == "" {
		prefix = "file"
	}
	return prefix + ext
}

// ExtFromMIME returns a file extension (including the dot) for the given MIME type.
// Returns ".bin" if the MIME type is unknown.
func ExtFromMIME(mime string) string {
	for ext, m := range mimeTypes {
		if m == mime {
			return ext
		}
	}
	return ".bin"
}
