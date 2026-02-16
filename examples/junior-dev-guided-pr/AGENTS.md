# Junior Dev: Guided Code Review & PR Preparation

> **Audience:** Junior Developers  
> **Scenario:** A junior developer has written a new REST API endpoint and needs help reviewing their code for quality issues, writing tests, and preparing a clean PR — all guided by the codebase's own coding standards.

---

## The Problem

You're a junior developer who just finished writing a new `CreateUser` endpoint. Before opening a PR, you need to:

1. Ensure your code follows the project's coding standards (from `Agents.md`)
2. Write unit tests with proper mocking (counterfeiter fakes)
3. Fix lint errors and ensure `golangci-lint` passes
4. Prepare a clean PR description

You're not sure about the project's patterns for error handling, request structs, or test organization. Genie's CodeOwner has already ingested the `Agents.md` standards and can guide you through the entire process.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                    CodeOwner (Mentor Agent)                   │
│  Persona: Software engineering assistant                     │
│  Context: Agents.md loaded as project coding standards       │
│  Memory:  Conversation history for multi-turn guidance       │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  Turn 1: Review code against Agents.md standards             │
│  Turn 2: Delegate test writing to sub-agent                  │
│  Turn 3: Delegate lint fixes to sub-agent                    │
│  Turn 4: Generate PR description                             │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                  Tool Registry                         │  │
│  │  • File tools (read, search, list, replace_content)    │  │
│  │  • Shell tool (go test, golangci-lint, go generate)    │  │
│  │  • Skill: code-review-checklist                        │  │
│  │  • MCP: github-mcp-server (create PR, push branch)    │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

---

## Configuration

### `genie.toml`

```toml
skills_roots = ["./skills"]

[mcp]
[[mcp.servers]]
name = "github"
transport = "stdio"
command = "npx"
args = ["-y", "@anthropic/github-mcp-server"]
env = { GITHUB_TOKEN = "${GITHUB_TOKEN}" }
```

---

## Interaction Flow

### Turn 1 — Ask for a code review

```
You: Review my new endpoint in pkg/service/user.go and pkg/handler/user.go.
     Check it against our project coding standards.
```

Genie reads `Agents.md` (auto-loaded into persona) and your files:

```
Genie: I've reviewed your code against the project standards. Here are the issues:

       ### Must Fix (from Agents.md rules)
       
       1. **Method signature violation** (§1 MANDATORY)
          `CreateUser(ctx context.Context, name string, email string)` has 4 params.
          → Must be: `CreateUser(ctx context.Context, req CreateUserRequest)`
          
       2. **Missing counterfeiter annotation** (§2 MANDATORY)
          `IUserService` interface has no `//counterfeiter:generate` comment.
          
       3. **Else block detected** (§4 MANDATORY)
          ```go
          // pkg/service/user.go:45
          if exists {
              return nil, ErrUserExists
          } else {
              return s.repo.Create(ctx, user)  // ← use early return instead
          }
          ```
          
       4. **Error not wrapped** (§Error Wrapping MANDATORY)
          `return err` on line 52 should be `return fmt.Errorf("creating user: %w", err)`

       5. **Missing godoc** (§3 MANDATORY)
          `CreateUser` method has no documentation comment.

       ### Recommendations
       - Use `problems.New` for business errors like "user already exists"
       - Add blind spot analysis: what if email is empty? What if duplicate detection races?

       Shall I fix these issues?
```

### Turn 2 — Fix issues and write tests

```
You: Yes, fix the issues and write unit tests using counterfeiter fakes.
```

Genie delegates the multi-file edits to a sub-agent:

```
# Sub-Agent 1: Fix code issues (write-heavy → delegate)
create_agent(
  goal="Fix the following in pkg/service/user.go and pkg/handler/user.go:
        1. Refactor CreateUser to accept CreateUserRequest struct
        2. Add //counterfeiter:generate annotation above IUserService
        3. Replace else block with early return
        4. Wrap errors with fmt.Errorf
        5. Add godoc comments to all exported functions
        6. Run 'go generate ./pkg/service/...' to regenerate fakes
        7. Run 'go build ./...' to verify compilation",
  task_type="tool_calling",
  tool_names=["read_file", "replace_content", "execute_command", "search_content"]
)

# Sub-Agent 2: Write unit tests (write-heavy → delegate)
create_agent(
  goal="Create pkg/service/user_test.go with unit tests for CreateUser:
        - Use Ginkgo/Gomega (check existing tests for pattern)
        - Use counterfeiter fake from pkg/repository/repositoryfakes/
        - Test cases: success, duplicate email, repository error, empty request
        - Follow project test patterns from Agents.md
        - Run 'go test ./pkg/service/...' to verify tests pass",
  task_type="tool_calling",
  tool_names=["read_file", "replace_content", "execute_command", "list_file", "search_content"]
)
```

### Turn 3 — Run lint and fix

```
You: Run the linter and fix any issues.
```

```
# Sub-Agent 3: Lint fix (shell-heavy → delegate)
create_agent(
  goal="Run 'golangci-lint run ./pkg/service/... ./pkg/handler/...' and fix all errors.
        If a command fails 3 times consecutively, stop and report.
        After fixing, re-run linter to confirm zero errors.",
  task_type="tool_calling",
  tool_names=["execute_command", "read_file", "replace_content"]
)
```

```
Genie: ✅ Linter passes with zero errors. Here's what was fixed:
       - Removed unused import `encoding/json` in user.go
       - Added error check on `rows.Close()` in repository
```

### Turn 4 — Prepare the PR

```
You: Create a PR with a good description.
```

Genie uses the GitHub MCP server and conversation memory:

```
Genie: I've created the PR using the github MCP tools:

       **PR #42: feat: add CreateUser endpoint with validation**
       
       ## Summary
       Adds a new REST endpoint `POST /api/v1/users` for user creation with 
       email uniqueness validation.
       
       ## Changes
       - `pkg/service/user.go` — CreateUser service method with request struct
       - `pkg/handler/user.go` — HTTP handler with input validation  
       - `pkg/service/user_test.go` — Unit tests (4 cases, 100% coverage)
       - `pkg/service/servicefakes/` — Regenerated counterfeiter fakes
       
       ## Coding Standards
       All changes verified against Agents.md:
       - ✅ 2-param method signatures (ctx + request struct)
       - ✅ Counterfeiter annotations  
       - ✅ No else blocks
       - ✅ Errors wrapped with fmt.Errorf
       - ✅ Godoc on all exports
       - ✅ golangci-lint passes
```

---

## Key Concepts Demonstrated

| Concept | How It's Used |
|---------|---------------|
| **Agents.md as persona** | CodeOwner auto-loads project standards and enforces them in reviews |
| **Conversation memory** | Multi-turn chat remembers context from review → fix → test → PR |
| **Sub-agent delegation** | Write-heavy tasks (fixing code, writing tests, lint) delegated to cheaper models |
| **Direct execution** | Code review and planning done by orchestrator (reasoning-heavy, no delegation) |
| **MCP tools** | GitHub MCP server for creating the PR branch and description |
| **Skills** | `code-review-checklist` skill can generate language-specific checklists |
| **Shell access** | `go test`, `golangci-lint`, `go generate` run directly |

---

## Why This Helps Junior Devs

1. **Standards enforcement** — No need to memorize `Agents.md`; Genie checks automatically
2. **Test pattern discovery** — Genie reads existing tests and replicates the project's patterns
3. **Build-test-fix loops** — Sub-agents iterate on compilation errors so you don't have to
4. **Learning by example** — Each fix includes the "why" from the coding standards section
5. **PR confidence** — Code is reviewed, tested, linted, and documented before you ask for human review
