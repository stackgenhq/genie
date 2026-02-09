#!/bin/bash
# Analyze Dockerfile for optimization opportunities

set -e

DOCKERFILE="$1"
OUTPUT_DIR="${OUTPUT_DIR:-./output}"

if [ -z "$DOCKERFILE" ]; then
    echo "Usage: $0 <path-to-dockerfile>"
    exit 1
fi

if [ ! -f "$DOCKERFILE" ]; then
    echo "Error: Dockerfile not found: $DOCKERFILE"
    exit 1
fi

mkdir -p "$OUTPUT_DIR"
REPORT="$OUTPUT_DIR/optimization_report.txt"

echo "Docker Optimization Report" > "$REPORT"
echo "==========================" >> "$REPORT"
echo "" >> "$REPORT"
echo "Analyzing: $DOCKERFILE" >> "$REPORT"
echo "Generated: $(date)" >> "$REPORT"
echo "" >> "$REPORT"

CRITICAL=0
HIGH=0
MEDIUM=0

# Check for 'latest' tag
if grep -q "FROM.*:latest" "$DOCKERFILE"; then
    echo "✗ CRITICAL: Using 'latest' tag for base image" >> "$REPORT"
    echo "  Recommendation: Use specific version tags for reproducibility" >> "$REPORT"
    echo "" >> "$REPORT"
    ((CRITICAL++))
fi

# Check for root user
if ! grep -q "USER " "$DOCKERFILE"; then
    echo "✗ CRITICAL: No USER directive found (running as root)" >> "$REPORT"
    echo "  Recommendation: Create and use a non-root user" >> "$REPORT"
    echo "  Example: RUN useradd -m appuser && USER appuser" >> "$REPORT"
    echo "" >> "$REPORT"
    ((CRITICAL++))
fi

# Check for multi-stage build
if ! grep -q "FROM.*AS" "$DOCKERFILE"; then
    echo "⚠ HIGH: Not using multi-stage build" >> "$REPORT"
    echo "  Recommendation: Use multi-stage builds to reduce final image size" >> "$REPORT"
    echo "  Can reduce image size by 50-80% for compiled languages" >> "$REPORT"
    echo "" >> "$REPORT"
    ((HIGH++))
fi

# Check for large base images
if grep -q "FROM ubuntu\|FROM centos\|FROM fedora" "$DOCKERFILE" && ! grep -q "slim\|alpine" "$DOCKERFILE"; then
    echo "⚠ HIGH: Using large base image" >> "$REPORT"
    echo "  Recommendation: Consider using Alpine or slim variants" >> "$REPORT"
    echo "  Example: ubuntu:22.04-slim, python:3.11-alpine" >> "$REPORT"
    echo "" >> "$REPORT"
    ((HIGH++))
fi

# Check for multiple RUN commands
RUN_COUNT=$(grep -c "^RUN " "$DOCKERFILE" || true)
if [ "$RUN_COUNT" -gt 5 ]; then
    echo "⚠ HIGH: Multiple RUN commands ($RUN_COUNT found)" >> "$REPORT"
    echo "  Recommendation: Combine RUN commands with && to reduce layers" >> "$REPORT"
    echo "" >> "$REPORT"
    ((HIGH++))
fi

# Check for COPY before package install
if grep -n "^COPY\|^ADD" "$DOCKERFILE" | head -1 | cut -d: -f1 | \
   xargs -I {} sh -c "grep -n '^RUN.*apt-get\|^RUN.*yum\|^RUN.*apk' \"$DOCKERFILE\" | head -1 | cut -d: -f1 | xargs -I [] test {} -lt []" 2>/dev/null; then
    echo "⚠ MEDIUM: COPY/ADD before package installation" >> "$REPORT"
    echo "  Recommendation: Install packages before copying application code" >> "$REPORT"
    echo "  This improves layer caching efficiency" >> "$REPORT"
    echo "" >> "$REPORT"
    ((MEDIUM++))
fi

# Check for apt-get update without clean
if grep -q "apt-get update" "$DOCKERFILE" && ! grep -q "rm -rf /var/lib/apt/lists" "$DOCKERFILE"; then
    echo "⚠ MEDIUM: apt-get update without cleanup" >> "$REPORT"
    echo "  Recommendation: Add 'rm -rf /var/lib/apt/lists/*' after apt-get" >> "$REPORT"
    echo "  This reduces image size" >> "$REPORT"
    echo "" >> "$REPORT"
    ((MEDIUM++))
fi

# Check for ADD instead of COPY
if grep -q "^ADD " "$DOCKERFILE" && ! grep -q "\.tar\|\.zip" "$DOCKERFILE"; then
    echo "⚠ MEDIUM: Using ADD instead of COPY" >> "$REPORT"
    echo "  Recommendation: Use COPY unless you need ADD's extraction features" >> "$REPORT"
    echo "" >> "$REPORT"
    ((MEDIUM++))
fi

# Summary
echo "" >> "$REPORT"
echo "SUMMARY" >> "$REPORT"
echo "=======" >> "$REPORT"
echo "Critical Issues: $CRITICAL" >> "$REPORT"
echo "High Priority: $HIGH" >> "$REPORT"
echo "Medium Priority: $MEDIUM" >> "$REPORT"
echo "" >> "$REPORT"

if [ $CRITICAL -eq 0 ] && [ $HIGH -eq 0 ] && [ $MEDIUM -eq 0 ]; then
    echo "✓ No major issues found! Dockerfile follows best practices." >> "$REPORT"
else
    echo "Total issues found: $((CRITICAL + HIGH + MEDIUM))" >> "$REPORT"
    echo "" >> "$REPORT"
    echo "PRIORITY ACTIONS:" >> "$REPORT"
    [ $CRITICAL -gt 0 ] && echo "1. Address $CRITICAL critical security/reliability issues" >> "$REPORT"
    [ $HIGH -gt 0 ] && echo "2. Implement $HIGH high-priority optimizations" >> "$REPORT"
    [ $MEDIUM -gt 0 ] && echo "3. Consider $MEDIUM medium-priority improvements" >> "$REPORT"
fi

echo "" >> "$REPORT"
echo "Report saved to: $REPORT"
cat "$REPORT"

exit 0
