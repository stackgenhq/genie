// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//
// Change Date: 2029-03-10
// Change License: Apache License, Version 2.0

package halguard

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

// --- Signal Weights ---
// Each signal contributes a weighted penalty to the fabrication score.
// Weights are calibrated so that a single strong signal (e.g. explicit
// role-play instruction) can push confidence below the default threshold,
// while weaker signals need to co-occur.
//
// Approach informed by PCC (Probabilistic Certainty and Consistency,
// arXiv:2503.xxxxx, 2025) and Semantic Inconsistency Index (SINdex):
// multi-signal scoring with weighted combination is more robust than
// any single binary classifier.

const (
	weightRolePlay           = 0.40 // Strong: explicit role-play instructions
	weightFabricationPattern = 0.25 // Medium: invented metrics/incidents
	weightSecondPerson       = 0.15 // Medium: "you are" framing (imperative role assignment)
	weightSpecificMetrics    = 0.15 // Medium: suspiciously specific numbers without tool backing
	weightInformationDensity = 0.10 // Weak: unusually high density of specific claims per sentence
	weightTemporalUrgency    = 0.10 // Weak: artificial urgency ("in the last 15 minutes")
)

// rolePlayPatterns are compiled regexes that detect role-play framing.
// Regexes are more flexible than exact string matches — they handle
// variations like "you're an SRE", "act as a DevOps engineer", etc.
var rolePlayPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(you are|you're|act as|pretend to be|imagine you'?re)\s+(an?\s+)?(sre|devops|software engineer|incident commander|on-call engineer)`),
	regexp.MustCompile(`(?i)\b(respond to|handle|triage|investigate)\s+(a |an |the )?(production |live |critical )?(incident|outage|alert)`),
	regexp.MustCompile(`(?i)\bhypothetical\s+scenario\b`),
	regexp.MustCompile(`(?i)\b(simulate|roleplay|role[\s-]play|tabletop exercise|drill exercise|mock incident)\b`),
	regexp.MustCompile(`(?i)\b(imagine|pretend|suppose)\s+(you|that|we)\b`),
	regexp.MustCompile(`(?i)\bin this scenario\b`),
}

// fabricationRegexes detect patterns suggesting invented operational data.
// More flexible than exact substring matches — handles number variations.
var fabricationRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)p\d{2,3}\s+latency\s+(spiked|increased|jumped|rose)\s+(from|to)\s+\d`),
	regexp.MustCompile(`(?i)(error rate|failure rate)\s+(jumped|spiked|increased)\s+(from|to)\s+[\d.]+`),
	regexp.MustCompile(`(?i)(cpu|memory|disk)\s+(usage|utilization)\s+(spiked|jumped|increased)\s+to\s+\d`),
	regexp.MustCompile(`(?i)in the last\s+\d+\s+(minutes?|hours?|seconds?)`),
	regexp.MustCompile(`(?i)(users are reporting|customers are complaining|the dashboard shows)`),
	regexp.MustCompile(`(?i)the service has been (down|unavailable|unreachable) for`),
}

// specificMetricPattern detects suspiciously precise numeric claims
// (e.g. "latency is 342ms", "error rate is 2.4%") that are likely
// fabricated when they appear in a goal without tool-call context.
var specificMetricPattern = regexp.MustCompile(`\d+(\.\d+)?\s*(%|ms|seconds?|minutes?|hours?|MB|GB|requests?/s|req/s|QPS|RPS|TPS)`)

// secondPersonRolePattern detects "You are..." framing that assigns
// a persona to the sub-agent — a strong indicator of role-play.
var secondPersonRolePattern = regexp.MustCompile(`(?i)^(you are|you're)\s`)

// temporalUrgencyPattern detects artificial time pressure language.
var temporalUrgencyPattern = regexp.MustCompile(`(?i)\b(right now|immediately|urgent|critical|production is down|pages? going off)\b`)

// scoreGoal analyses a goal string and returns a fabrication penalty [0, 1]
// together with the individual signal contributions. 0 means no fabrication
// signals detected; 1 means extremely likely fabricated. The caller should
// use 1 - penalty as the confidence score.
func scoreGoal(goal string) (penalty float64, signals GroundingSignals) {
	// Signal 1: Role-play patterns (strongest signal).
	rolePlayScore := scoreRegexMatches(goal, rolePlayPatterns)
	if rolePlayScore > 0 {
		signals.RolePlay = rolePlayScore * weightRolePlay
	}

	// Signal 2: Fabrication patterns (invented metrics/incidents).
	fabricationScore := scoreRegexMatches(goal, fabricationRegexes)
	if fabricationScore > 0 {
		signals.FabricationPattern = fabricationScore * weightFabricationPattern
	}

	// Signal 3: Second-person role assignment at sentence start.
	if secondPersonRolePattern.MatchString(strings.TrimSpace(goal)) {
		signals.SecondPersonRole = weightSecondPerson
	}

	// Signal 4: Suspiciously specific metrics without tool context.
	metricMatches := specificMetricPattern.FindAllString(goal, -1)
	if len(metricMatches) >= 2 {
		density := float64(len(metricMatches)) / float64(max(1, countSentences(goal)))
		signals.SpecificMetrics = min(1.0, density*0.5) * weightSpecificMetrics
	}

	// Signal 5: Information density — many specific claims per sentence.
	// This is the primary safety net when fabrication regexes are bypassed by
	// creative rephrasing. It combines:
	//   (a) specific-term density (numbers, technical vocabulary)
	//   (b) numeric cluster density (multiple numbers in a small window,
	//       e.g. "from 50ms to 2000ms" reworded as "degraded by a factor of 40")
	//   (c) narrative arc score (cause→effect→action packed in one message)
	infoDensity := informationDensity(goal)
	narrative := narrativeArcScore(goal)
	combinedDensity := math.Max(infoDensity, (infoDensity+narrative)/2)
	if combinedDensity > 0.5 {
		signals.InformationDensity = min(1.0, (combinedDensity-0.5)*2) * weightInformationDensity
	}

	// Signal 6: Temporal urgency.
	if temporalUrgencyPattern.MatchString(goal) {
		signals.TemporalUrgency = weightTemporalUrgency
	}

	penalty = signals.Penalty()
	return penalty, signals
}

// scoreRegexMatches returns a score in [0, 1] based on how many patterns match.
// One match returns 0.7; each additional match adds up to 1.0.
func scoreRegexMatches(text string, patterns []*regexp.Regexp) float64 {
	matches := 0
	for _, p := range patterns {
		if p.MatchString(text) {
			matches++
		}
	}
	if matches == 0 {
		return 0
	}
	// Diminishing returns: first match is strongest, extras add less.
	return math.Min(1.0, 0.7+float64(matches-1)*0.15)
}

// informationDensity estimates how "packed" a text is with specific claims.
// A high density of named entities, numbers, and technical terms in a short
// text suggests fabrication. Returns a value in [0, 1].
func informationDensity(text string) float64 {
	words := strings.Fields(text)
	if len(words) < 5 {
		return 0
	}

	specifics := 0
	for _, w := range words {
		w = strings.ToLower(strings.TrimFunc(w, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}))
		if isSpecificTerm(w) {
			specifics++
		}
	}

	return float64(specifics) / float64(len(words))
}

// isSpecificTerm returns true for words that contribute to information density.
func isSpecificTerm(word string) bool {
	// Numbers and version-like strings.
	if len(word) > 0 && (word[0] >= '0' && word[0] <= '9') {
		return true
	}
	// "v2.8.4" style version strings.
	if len(word) > 1 && (word[0] == 'v' || word[0] == 'V') && word[1] >= '0' && word[1] <= '9' {
		return true
	}
	// Technical/operational terms — broad categories, not exhaustive matches.
	// This catches variations: "latencies", "cpu-bound", etc.
	techPrefixes := []string{
		"latenc", "throughput", "error", "cpu", "memory", "mem",
		"disk", "timeout", "spike", "alert", "incident", "outage",
		"deploy", "rollback", "service", "pod", "container", "node",
		"cluster", "database", "queue", "cache", "replica", "shard",
		"degrad", "crash", "fail", "leak", "oom", "restart",
		"metric", "monitor", "grafana", "prometheus", "dashboard",
		"request", "response", "bandwidth", "saturat",
	}
	for _, p := range techPrefixes {
		if strings.HasPrefix(word, p) {
			return true
		}
	}
	return false
}

// narrativeArcScore detects the cause→effect→action pattern typical of
// fabricated incident scenarios. Fabricated goals tend to pack all three
// phases into a single message, which is structurally unusual for genuine
// requests. Returns [0, 1].
func narrativeArcScore(text string) float64 {
	lower := strings.ToLower(text)
	score := 0.0

	// Phase 1: Situation/State description ("X is/was/has been Y")
	situationMarkers := []string{
		"is down", "was down", "has been", "went down", "is failing",
		"started to", "began to", "noticed that", "observed that",
		"we detected", "we noticed", "we saw", "we observed",
		"appears to be", "seems to", "looks like",
	}
	for _, m := range situationMarkers {
		if strings.Contains(lower, m) {
			score += 0.35
			break
		}
	}

	// Phase 2: Cause identification ("caused by", "due to", "because")
	causeMarkers := []string{
		"caused by", "due to", "because", "root cause",
		"introduced in", "resulted from", "triggered by",
		"following the", "after the", "since the",
	}
	for _, m := range causeMarkers {
		if strings.Contains(lower, m) {
			score += 0.35
			break
		}
	}

	// Phase 3: Action directive ("investigate", "fix", "check")
	actionMarkers := []string{
		"investigate", "fix", "check", "diagnose", "resolve",
		"mitigate", "remediate", "troubleshoot", "debug",
		"generate a report", "write a report", "create a report",
		"find out why", "figure out", "determine why",
	}
	for _, m := range actionMarkers {
		if strings.Contains(lower, m) {
			score += 0.3
			break
		}
	}

	return min(1.0, score)
}

// countSentences counts approximate sentence boundaries in text.
func countSentences(text string) int {
	count := 0
	for _, r := range text {
		if r == '.' || r == '?' || r == '!' {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}

// segmentIntoBlocks splits text into semantic blocks at sentence boundaries.
// Each block is a non-empty, trimmed sentence. Headers (lines starting with #)
// are kept as separate blocks.
func segmentIntoBlocks(text string) []string {
	var blocks []string
	var current strings.Builder

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if current.Len() > 0 {
				blocks = append(blocks, strings.TrimSpace(current.String()))
				current.Reset()
			}
			continue
		}

		// Headers and list items get their own blocks.
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			if current.Len() > 0 {
				blocks = append(blocks, strings.TrimSpace(current.String()))
				current.Reset()
			}
			blocks = append(blocks, trimmed)
			continue
		}

		// Accumulate into the current block.
		if current.Len() > 0 {
			current.WriteRune(' ')
		}
		current.WriteString(trimmed)

		// Split on sentence-ending punctuation.
		if endsWithSentence(trimmed) {
			blocks = append(blocks, strings.TrimSpace(current.String()))
			current.Reset()
		}
	}
	if current.Len() > 0 {
		blocks = append(blocks, strings.TrimSpace(current.String()))
	}

	return blocks
}

// endsWithSentence reports whether the text ends with sentence-ending
// punctuation (period, question mark, exclamation mark) possibly followed
// by a closing quote or parenthesis.
func endsWithSentence(s string) bool {
	s = strings.TrimRightFunc(s, func(r rune) bool {
		return r == '"' || r == '\'' || r == ')' || r == ']' || unicode.IsSpace(r)
	})
	if s == "" {
		return false
	}
	last := s[len(s)-1]
	return last == '.' || last == '?' || last == '!'
}
