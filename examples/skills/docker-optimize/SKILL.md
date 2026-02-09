---
name: docker-optimize
description: Analyze and optimize Dockerfiles for better performance, security, and smaller image sizes
---

# Docker Optimize

Analyzes Dockerfiles and provides actionable recommendations for optimization including multi-stage builds, layer caching, security best practices, and image size reduction.

## When to Use This Skill

Use this skill when you need to:
- Reduce Docker image sizes
- Improve build performance
- Enhance container security
- Implement multi-stage builds
- Optimize layer caching
- Follow Docker best practices

## Usage

### Analyze Dockerfile

```bash
bash scripts/analyze.sh <path-to-dockerfile>
```

The script will analyze the Dockerfile and generate a comprehensive report with:
- Image size optimization opportunities
- Security vulnerabilities and fixes
- Build performance improvements
- Layer caching recommendations
- Multi-stage build suggestions

## What the Analysis Checks

### 1. Base Image Selection
- Checks for Alpine or slim variants
- Identifies oversized base images
- Suggests minimal alternatives

### 2. Layer Optimization
- Detects inefficient RUN commands
- Suggests command chaining
- Identifies cache-busting patterns

### 3. Security Best Practices
- Checks for running as root
- Validates COPY vs ADD usage
- Identifies hardcoded secrets
- Checks for latest tag usage

### 4. Multi-Stage Builds
- Detects opportunities for multi-stage builds
- Suggests build/runtime separation
- Identifies unnecessary build dependencies in final image

### 5. Build Performance
- Checks COPY/ADD order for cache efficiency
- Identifies unnecessary package installations
- Suggests .dockerignore usage

## Output

The script generates:
- `optimization_report.txt`: Detailed analysis with recommendations
- `optimized_dockerfile`: Suggested optimized version (if applicable)
- `size_comparison.json`: Before/after size estimates

## Example Output

```
Docker Optimization Report
==========================

Image: myapp:latest
Current estimated size: 1.2GB

CRITICAL ISSUES (3):
  ✗ Using 'latest' tag for base image
  ✗ Running as root user
  ✗ Installing unnecessary packages

HIGH PRIORITY (5):
  ⚠ Not using multi-stage build
  ⚠ Inefficient layer caching
  ⚠ Large base image (ubuntu:latest)
  ⚠ Multiple RUN commands can be combined
  ⚠ COPY before package install breaks cache

RECOMMENDATIONS:
  1. Use ubuntu:22.04-slim instead of ubuntu:latest
  2. Implement multi-stage build (estimated 60% size reduction)
  3. Create non-root user for runtime
  4. Combine RUN commands to reduce layers
  5. Move COPY commands after package installation

Potential size reduction: ~700MB (58%)
```

## Dependencies

- bash 4.0+
- docker CLI (for optional size verification)

## Best Practices Applied

This skill follows Docker's official best practices:
- Minimize layer count
- Use .dockerignore
- Don't install unnecessary packages
- Use multi-stage builds
- Run as non-root user
- Use specific tags, not 'latest'
- Leverage build cache
