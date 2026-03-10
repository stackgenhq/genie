// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package youtubetranscript provides a tool to fetch transcript/captions from
// YouTube videos. It mimics how other systems (e.g. LangChain YoutubeLoader,
// youtube-transcript-api) do it: fetch the watch page, extract the caption
// track URL from the player response, then fetch the caption content.
// No YouTube API key or external transcript service is required.
package youtubetranscript

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/httputil"
	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	requestTimeout = 25 * time.Second
	maxBodyBytes   = 2 << 20 // 2 MB for watch page, 512 KB for captions
	userAgent      = "Mozilla/5.0 (compatible; Genie/1.0; +https://stackgen.com)"
)

// Request is the input for the youtube_transcript tool.
type Request struct {
	VideoURL  string `json:"video_url" jsonschema:"description=YouTube video URL (e.g. https://www.youtube.com/watch?v=VIDEO_ID or https://youtu.be/VIDEO_ID),required"`
	Language  string `json:"language,omitempty" jsonschema:"description=Preferred language code (e.g. en, es). If empty, uses the first available track (often the default or English)."`
	WithTimes bool   `json:"with_timestamps,omitempty" jsonschema:"description=If true, include timestamps in the output (e.g. [00:01:23] text). Default false."`
}

// NewTool returns a callable tool that fetches YouTube video transcripts.
// Use this when the user asks to transcribe, scribe, or get captions from a YouTube video.
func NewTool() tool.CallableTool {
	return function.NewFunctionTool(
		doFetch,
		function.WithName("youtube_transcript"),
		function.WithDescription(
			"Fetch the transcript (captions/subtitles) of a YouTube video. "+
				"Give the video URL (e.g. https://www.youtube.com/watch?v=... or https://youtu.be/...). "+
				"Returns the transcript text so you can summarize, quote, or answer questions about the video. "+
				"Works for videos that have captions (manual or auto-generated).",
		),
	)
}

func doFetch(ctx context.Context, req Request) (string, error) {
	log := logger.GetLogger(ctx).With("fn", "youtube_transcript")

	if strings.TrimSpace(req.VideoURL) == "" {
		return "", fmt.Errorf("video_url is required")
	}

	videoID, err := extractVideoID(req.VideoURL)
	if err != nil {
		return "", err
	}

	client := httputil.GetClient()

	// Step 1: Fetch watch page and extract caption track baseUrl.
	watchURL := "https://www.youtube.com/watch?v=" + videoID
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, watchURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("User-Agent", userAgent)

	log.Info("fetching watch page for caption tracks", "video_id", videoID)
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("fetch watch page: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("watch page returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read watch page: %w", err)
	}

	baseURL, err := extractCaptionBaseURL(body, req.Language)
	if err != nil {
		return "", err
	}

	// Step 2: Fetch caption content (JSON3 format).
	captionURL := baseURL
	if !strings.Contains(captionURL, "fmt=") {
		if strings.Contains(captionURL, "?") {
			captionURL += "&fmt=json3"
		} else {
			captionURL += "?fmt=json3"
		}
	}

	httpReq2, err := http.NewRequestWithContext(ctx, http.MethodGet, captionURL, nil)
	if err != nil {
		return "", fmt.Errorf("build caption request: %w", err)
	}
	httpReq2.Header.Set("User-Agent", userAgent)
	httpReq2.Header.Set("Referer", "https://www.youtube.com/")

	resp2, err := client.Do(httpReq2)
	if err != nil {
		return "", fmt.Errorf("fetch captions: %w", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusOK {
		return "", fmt.Errorf("captions returned HTTP %d", resp2.StatusCode)
	}

	capBody, err := io.ReadAll(io.LimitReader(resp2.Body, 512*1024))
	if err != nil {
		return "", fmt.Errorf("read captions: %w", err)
	}

	text, err := parseJSON3Captions(capBody, req.WithTimes)
	if err != nil {
		return "", fmt.Errorf("parse captions: %w", err)
	}

	log.Info("youtube transcript fetched", "video_id", videoID, "length", len(text))
	return text, nil
}

// extractVideoID returns the YouTube video ID from common URL formats.
func extractVideoID(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	// youtu.be/VIDEO_ID
	if (u.Host == "youtu.be" || strings.HasSuffix(u.Host, ".youtu.be")) && u.Path != "" {
		id := strings.TrimPrefix(u.Path, "/")
		if idx := strings.Index(id, "?"); idx >= 0 {
			id = id[:idx]
		}
		if id != "" {
			return id, nil
		}
	}

	// youtube.com/watch?v=VIDEO_ID or youtube.com/embed/VIDEO_ID
	if strings.Contains(u.Host, "youtube.com") {
		if v := u.Query().Get("v"); v != "" {
			return v, nil
		}
		if strings.HasPrefix(u.Path, "/embed/") {
			parts := strings.SplitN(strings.TrimPrefix(u.Path, "/embed/"), "/", 2)
			if parts[0] != "" {
				return parts[0], nil
			}
		}
	}

	return "", fmt.Errorf("could not extract video ID from URL (use youtube.com/watch?v=... or youtu.be/...)")
}

// extractCaptionBaseURL finds a caption track baseUrl in the watch page body.
// Prefers language match if preferLang is set; otherwise returns the first timedtext URL.
var (
	reBaseURL  = regexp.MustCompile(`"baseUrl"\s*:\s*"([^"]+)"`)
	reLangBase = regexp.MustCompile(`"languageCode"\s*:\s*"([^"]+)"[^}]*"baseUrl"\s*:\s*"([^"]+)"`)
	reBaseLang = regexp.MustCompile(`"baseUrl"\s*:\s*"([^"]+)"[^}]*"languageCode"\s*:\s*"([^"]+)"`)
)

func extractCaptionBaseURL(body []byte, preferLang string) (string, error) {
	preferLang = strings.ToLower(strings.TrimSpace(preferLang))
	var firstTrack string

	// Try to find (languageCode, baseUrl) or (baseUrl, languageCode) pairs.
	for _, re := range []*regexp.Regexp{reLangBase, reBaseLang} {
		matches := re.FindAllSubmatch(body, -1)
		for _, m := range matches {
			if len(m) < 3 {
				continue
			}
			var lang, base string
			if re == reLangBase {
				lang, base = string(m[1]), string(m[2])
			} else {
				base, lang = string(m[1]), string(m[2])
			}
			if !strings.Contains(base, "timedtext") {
				continue
			}
			if firstTrack == "" {
				firstTrack = base
			}
			if preferLang != "" && strings.ToLower(lang) == preferLang {
				return base, nil
			}
			if preferLang == "" {
				return base, nil
			}
		}
	}

	if firstTrack != "" {
		return firstTrack, nil
	}
	// Fallback: first baseUrl that looks like a timedtext URL.
	for _, m := range reBaseURL.FindAllSubmatch(body, -1) {
		if len(m) >= 2 && strings.Contains(string(m[1]), "timedtext") {
			return string(m[1]), nil
		}
	}
	return "", fmt.Errorf("no caption tracks found (video may have captions disabled)")
}

// json3CaptionResponse is the structure of YouTube's timedtext fmt=json3 response.
type json3CaptionResponse struct {
	Events []struct {
		TStartMs int64 `json:"tStartMs"`
		Duration int64 `json:"dDurationMs"`
		Segments []struct {
			UTF8 string `json:"utf8"`
		} `json:"segs"`
	} `json:"events"`
}

func parseJSON3Captions(body []byte, withTimes bool) (string, error) {
	var data json3CaptionResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, ev := range data.Events {
		for _, seg := range ev.Segments {
			t := strings.TrimSpace(seg.UTF8)
			if t == "" || t == "\n" {
				continue
			}
			if withTimes && ev.TStartMs >= 0 {
				sec := ev.TStartMs / 1000
				fmt.Fprintf(&sb, "[%02d:%02d:%02d] ", sec/3600, (sec%3600)/60, sec%60)
			}
			sb.WriteString(t)
			if !strings.HasSuffix(t, "\n") {
				sb.WriteString(" ")
			}
		}
	}
	return strings.TrimSpace(sb.String()), nil
}
