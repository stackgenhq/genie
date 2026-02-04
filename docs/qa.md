# QA Testing Guide for Genie TUI

This guide helps you quickly test changes to the Genie TUI and grant command during development.

## Prerequisites

### Environment Setup

The genie command requires API keys to be set. Create a `.env` file in the repo root:

```bash
# .env file
OPENAI_API_KEY=sk-...
# Or use GEMINI_API_KEY if using Gemini
# GEMINI_API_KEY=...
```

**Load environment before running:**

```bash
# Source the .env file
set -a && source .env && set +a

# Now run genie commands
go run --mod=mod ./main.go

 go run  --mod=mod ./main.go --code-dir=/Users/sabithks/src/github.com/awsdocs/aws-doc-sdk-examples/javav2/example_code/sqs
```

## Quick Test Commands

> **⚠️ Known Issue**: There's currently a flag binding bug. The workaround is to use the default directories or build the binary first.

### Workaround Option 1: Use Default Directories

```bash
# Navigate to the directory you want to analyze
cd /Users/sabithks/src/github.com/awsdocs/aws-doc-sdk-examples/javav2/example_code/sqs

# Source .env and run from that directory
set -a && source /path/to/genie/.env && set +a
go run --mod=mod /path/to/genie/main.go grant

# Output will be in ./genie_output
```

### Workaround Option 2: Build Binary First

```bash
# Build the binary
cd /path/to/genie
go build --mod=mod -o ./bin/genie ./main.go

# Run with explicit flags (this works with the binary)
set -a && source .env && set +a
./bin/genie --code-dir=/Users/sabithks/src/github.com/awsdocs/aws-doc-sdk-examples/javav2/example_code/sqs --save-to=/tmp/genie-test grant
```

### 1. **Test with Default Directories** (Current Working Directory)

```bash
# From the genie repo root
go run --mod=mod ./main.go grant
```

**Expected**: Should analyze the current directory and output to `./genie_output`

### 2. **Test with Specific Code Directory**

```bash
# Test with AWS SDK examples
. .env
go run --mod=mod ./main.go --code-dir=/Users/sabithks/src/github.com/awsdocs/aws-doc-sdk-examples/javav2/example_code/sqs --save-to=/tmp/genie-test grant
```

### 3. **Test with Debug Logging**

```bash
# Enable debug logs to see detailed output
go run --mod=mod ./main.go --log-level=debug grant
```

### 4. **Build and Test Binary**

```bash
# Build the binary
go build --mod=mod -o ./bin/genie ./main.go

# Run the binary
./bin/genie --code-dir=/path/to/code --save-to=/tmp/output grant
```

## TUI-Specific Testing

### Verify TUI Features

When running the grant command, verify these TUI elements:

#### ✅ **Header**
- [ ] Shows "🧞 genie - Your Intent is My Command"
- [ ] Displays elapsed time (updates every second)
- [ ] Format: `⏱️  Elapsed: 00m 15s`

#### ✅ **Progress Bar**
- [ ] Shows current stage (Analyzing, Architecting, Generating, Complete)
- [ ] Uses symbols: `✓` (complete), `▶` (current), `○` (pending)
- [ ] Example: `✓ Analyzing  ▶ Architecting  ○ Generating  ○ Complete`

#### ✅ **Agent Output**
- [ ] Shows thinking messages: `🤖 Architect is thinking...`
- [ ] Displays tool calls with status
- [ ] Shows completion messages

#### ✅ **Log Panel**
- [ ] Bordered panel with title "📋 System Logs"
- [ ] Color-coded logs:
  - DEBUG: Gray/muted
  - INFO: Cyan
  - WARN: Yellow
  - ERROR: Red (bold)
- [ ] Timestamp format: `[HH:MM:SS]`
- [ ] No noisy/repetitive messages

#### ✅ **Keyboard Navigation**
- [ ] `↑` or `k` - Scroll logs up
- [ ] `↓` or `j` - Scroll logs down
- [ ] `PgUp` - Page up in logs
- [ ] `PgDn` - Page down in logs
- [ ] `Ctrl+C` or `q` - Quit

#### ✅ **Footer**
- [ ] Shows keyboard shortcuts
- [ ] Example: `Press Ctrl+C • ↑/↓ or j/k to scroll • PgUp/PgDn for pages`

## Common Issues and Fixes

### Issue: Blank/Black Screen

**Symptoms**: TUI shows mostly black screen with only errors at bottom

**Fixes**:
1. Check terminal size (minimum 80x24)
2. Verify no excessive logging to stderr
3. Check that `m.width` is being set (should use default 120 if not)

**Debug**:
```bash
# Check if window size is detected
# Add temporary debug log in model.go Update method for tea.WindowSizeMsg
```

### Issue: Repetitive Log Messages

**Symptoms**: Logs show repeated "method call", "could not map", etc.

**Fixes**:
1. Verify `shouldSkipMessage()` in `slog_handler.go` is working
2. Check logrus levels are set to `FatalLevel` in:
   - `pkg/mcputils/tool.go`
   - `pkg/iacgen/generator/terraform_tools.go`

**Debug**:
```bash
# Temporarily add print statement in shouldSkipMessage
fmt.Fprintf(os.Stderr, "Filtering: %s\n", msg)
```

### Issue: Logs Not Appearing

**Symptoms**: Log panel is empty or not showing

**Fixes**:
1. Verify slog handler is set up: `tui.SetupTUILogger(eventChan, slog.LevelInfo)`
2. Check `logger.SetLogHandler(tuiHandler)` is called
3. Ensure `LogMsg` events are being sent to event channel

**Debug**:
```bash
# Add debug output in Handle method of slog_handler.go
fmt.Fprintf(os.Stderr, "Log: [%s] %s\n", r.Level, r.Message)
```

### Issue: "missing required flags" Error

**Symptoms**: Command fails with "missing required flags: code-dir, save-to"

**Current Status**: Known bug - flag binding issue between root and grant commands

**Workaround**: Use explicit flags:
```bash
go run --mod=mod ./main.go --code-dir=$(pwd) --save-to=./genie_output grant
```

## Test Scenarios

### Scenario 1: Java SQS Example (Quick Test)

```bash
cd /Users/sabithks/src/github.com/appcd-dev/genie
go run --mod=mod ./main.go \
  --code-dir=/Users/sabithks/src/github.com/awsdocs/aws-doc-sdk-examples/javav2/example_code/sqs \
  --save-to=/tmp/genie-sqs-test \
  grant
```

**Expected Output**:
- Analyzes Java SQS code
- Generates Terraform for SQS infrastructure
- Shows real-time progress in TUI
- Logs appear in scrollable panel

**Validation**:
```bash
# Check output files
ls -la /tmp/genie-sqs-test/
cat /tmp/genie-sqs-test/*.tf
```

### Scenario 2: Empty Directory (Error Handling)

```bash
mkdir -p /tmp/empty-test
go run --mod=mod ./main.go \
  --code-dir=/tmp/empty-test \
  --save-to=/tmp/genie-empty-test \
  grant
```

**Expected**: Should handle gracefully with appropriate error message

### Scenario 3: Large Codebase (Performance)

```bash
# Test with larger codebase
go run --mod=mod ./main.go \
  --code-dir=/Users/sabithks/src/github.com/awsdocs/aws-doc-sdk-examples/javav2 \
  --save-to=/tmp/genie-large-test \
  grant
```

**Monitor**:
- Elapsed time stays accurate
- Logs don't overflow (should filter noise)
- TUI remains responsive

## Automated Testing

### Unit Tests

```bash
# Run all tests
go test --mod=mod ./...

# Run TUI-specific tests
go test --mod=mod ./pkg/tui/...

# Run with coverage
go test --mod=mod -cover ./pkg/tui/...
```

### Integration Tests

```bash
# Test grant command end-to-end
go test --mod=mod ./cmd/... -v
```

## Performance Benchmarks

### Measure TUI Overhead

```bash
# Without TUI (if you have a non-TUI mode)
time go run --mod=mod ./main.go grant

# With TUI
time go run --mod=mod ./main.go grant
```

**Expected**: TUI should add < 100ms overhead

### Log Throughput

Test with high-volume logging:
```bash
# Temporarily increase log verbosity
go run --mod=mod ./main.go --log-level=debug grant
```

**Monitor**:
- No dropped logs (check event channel buffer)
- Smooth scrolling
- No UI freezes

## Regression Checklist

Before committing TUI changes, verify:

- [ ] TUI renders on first frame (no blank screen)
- [ ] Elapsed time updates every second
- [ ] Progress bar shows correct stages
- [ ] Logs are color-coded correctly
- [ ] Keyboard navigation works (↑/↓, PgUp/PgDn)
- [ ] No excessive/repetitive log messages
- [ ] Ctrl+C exits cleanly
- [ ] Final state shows completion message
- [ ] Output files are generated correctly

## Debugging Tips

### Enable Verbose Output

```bash
# Set environment variable for more details
export GENIE_DEBUG=1
go run --mod=mod ./main.go grant
```

### Capture TUI Recording

```bash
# Use asciinema to record terminal session
asciinema rec genie-test.cast
go run --mod=mod ./main.go grant
# Ctrl+D to stop recording

# Replay
asciinema play genie-test.cast
```

### Check Event Flow

Add temporary logging in `pkg/tui/model.go`:

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    fmt.Fprintf(os.Stderr, "MSG: %T\n", msg)  // Debug: see all messages
    // ... rest of Update
}
```

## Quick Fixes Reference

| Issue | File | Fix |
|-------|------|-----|
| Blank screen | `pkg/tui/model.go` | Use default width (120) if `m.width == 0` |
| Noisy logs | `pkg/tui/slog_handler.go` | Add patterns to `shouldSkipMessage()` |
| Logrus noise | `pkg/mcputils/tool.go` | Set level to `FatalLevel` |
| No logs | `cmd/grant.go` | Call `tui.SetupTUILogger()` and `logger.SetLogHandler()` |
| Viewport not scrolling | `pkg/tui/model.go` | Check `logViewport.SetContent()` is called |

## Contact

For TUI-related issues, check:
- [walkthrough.md](file:///Users/sabithks/.gemini/antigravity/brain/b715bd50-4f27-46bd-b733-e89e3675be81/walkthrough.md) - Implementation details
- [tui_fix.md](file:///Users/sabithks/.gemini/antigravity/brain/b715bd50-4f27-46bd-b733-e89e3675be81/tui_fix.md) - Recent fixes
