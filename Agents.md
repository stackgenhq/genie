# Agent Coding Standards and Guidelines

This document outlines the mandatory coding standards and guidelines that must be followed when contributing to Go-based projects. These rules ensure consistency, testability, and maintainability across the codebase.

## Table of Contents

- [Build Instructions](#build-instructions)
- [Golang Coding Standards](#golang-coding-standards)
- [Domain Driven Design](#domain-driven-design)
- [Code Quality Standards](#code-quality-standards)
- [Defensive Coding Standards](#defensive-coding-standards)
- [Documentation Standards](#documentation-standards)
- [Testing Requirements](#testing-requirements)
  - [General Testing Rules](#general-testing-rules)
  - [Golang Testing](#golang-testing)
- [GitHub Pull Request Workflow](#github-pull-request-workflow)
  - [Addressing Review Comments](#1-addressing-review-comments-mandatory)
- [Reference Examples](#reference-examples)
- [Acceptance Criteria](#acceptance-criteria)

## Build Instructions

### Handling Tree-Sitter Dependencies

**When building or testing code that depends on tree-sitter packages, use the `-mod=mod` flag.**

#### Rules

- **Tree-Sitter Compilation**: For any `go build`, `go test`, or `go run` commands that involve tree-sitter dependencies, add the `-mod=mod` flag
- **Example Commands**:
  - `go build -mod=mod ./...`
  - `go test -mod=mod ./...`
  - `go run -mod=mod ./examples/skills_example.go`

#### Why This Is Needed

The tree-sitter Go bindings require C source files that are not always available in vendored dependencies. The `-mod=mod` flag ensures Go uses the module cache where these files are properly available.

#### Common Error Without Flag

```
fatal error: '../../src/parser.c' file not found
fatal error: 'tree_sitter/api.h' file not found
```

If you see these errors, add `-mod=mod` to your build command.

## Golang Coding Standards

### 1. Method Signature Pattern (MANDATORY)

**All Golang interface methods MUST follow a strict 2-parameter pattern:**

1. **First parameter**: `ctx context.Context` (always required)
2. **Second parameter**: A request struct containing all necessary parameters

#### Rules

- **NO exceptions**: All interface methods must have exactly 2 parameters
- The first parameter must always be `ctx context.Context`
- The second parameter must be a struct type (not individual parameters)
- Return values can vary based on the method's needs

#### Interface Pattern Examples

- **Correct interface pattern**: See [`pkg/repository/slo.go`](pkg/repository/slo.go) (lines 49-54) - `ISloRepo` interface with `ctx context.Context` as first parameter and request struct as second parameter
- **Request struct examples**: See [`pkg/repository/slo.go`](pkg/repository/slo.go) for `SloFilter` struct definition and usage

#### Incorrect Patterns to Avoid

- ❌ Missing context parameter
- ❌ Individual parameters instead of a request struct
- ❌ Context not as the first parameter
- ❌ More than 2 parameters (ctx + request struct only)

### 2. Counterfeiter Annotations (MANDATORY)

**All Golang interfaces MUST have counterfeiter annotations** to generate fakes for easier unit testing.

#### Rules

- Add `//counterfeiter:generate . InterfaceName` comment above each interface
- Run `go generate ./...` to generate fake implementations
- Fakes are generated in `fakes/` directories
- Use fakes in unit tests instead of manual mocks

#### Counterfeiter Examples

- **Counterfeiter annotations**: See [`pkg/repository/fakes.go`](pkg/repository/fakes.go) for how counterfeiter annotations are organized for all interfaces
- **Interface with annotation**: See [`pkg/repository/slo.go`](pkg/repository/slo.go) (lines 42-48) for `ISloRepo` interface definition with counterfeiter annotation
- **Using fakes in tests**: See [`pkg/service/slo_test.go`](pkg/service/slo_test.go) (lines 22-32) for an example of using counterfeiter-generated fakes
- **Generated fake**: See [`pkg/repository/repositoryfakes/fake_islo_repo.go`](pkg/repository/repositoryfakes/fake_islo_repo.go) for the generated fake implementation

### 3. Avoid Package-Level Functions (MANDATORY)

**All Golang functions MUST be methods on types (structs) rather than package-level functions.**

#### Rules

- **No Package-Level Functions**: Avoid creating package-level functions unless absolutely necessary (e.g., constructors like `NewX()`)
- **Use Methods**: Prefer methods on types/structs over package-level functions
- **Better Testability**: Methods on types are easier to test and mock
- **Clear Ownership**: Methods make it clear which type owns the functionality
- **Dependency Injection**: Methods on types enable better dependency injection patterns

#### Exceptions

The following are acceptable as package-level functions:

- Constructor functions: `NewX()`, `NewXWithConfig()`, etc.
- Utility functions that are truly stateless and have no dependencies
- Functions that are part of a package's public API and don't logically belong to any type

#### Method Pattern Examples

**Correct - Method on type:**

```go
type FileLoader struct{}

func (f *FileLoader) LoadOpenSLOFromFile(filePath string) (*OpenSLOSpec, error) {
    // implementation
}

type SyncOptions struct {
    Directory string
}

func (s *SyncOptions) findSLOFiles() ([]string, error) {
    // implementation
}
```

**Incorrect - Package-level function:**

```go
// ❌ Don't do this
func findSLOFiles(directory string) ([]string, error) {
    // implementation
}

// ✅ Do this instead
type SyncOptions struct {
    Directory string
}

func (s *SyncOptions) findSLOFiles() ([]string, error) {
    // implementation
}
```

#### Benefits of Methods Over Package Functions

- **Testability**: Methods can be tested by creating instances of the type
- **State Management**: Methods can access and modify struct fields
- **Dependency Injection**: Methods enable dependency injection through struct fields
- **Extensibility**: Methods can be overridden or extended through interfaces
- **Clarity**: Methods make it clear which type is responsible for the functionality

#### Anti-Patterns to Avoid

- ❌ Creating package-level functions when a method would be more appropriate
- ❌ Using package-level functions that take many parameters (use a struct method instead)
- ❌ Package-level functions that could benefit from state or configuration
- ❌ Package-level functions that are hard to test or mock

#### Examples

- **Correct pattern**: See [`pkg/slocmd/sync.go`](pkg/slocmd/sync.go) - `findSLOFiles()` is a method on `SyncOptions` struct
- **Correct pattern**: See [`pkg/slocmd/file_loader.go`](pkg/slocmd/file_loader.go) - `LoadOpenSLOFromFile()` is a method on `FileLoader` struct

### 4. Avoid Else Blocks (MANDATORY)

**All Golang code MUST avoid using `else` blocks. Use early returns or guard clauses instead.**

#### Rules

- **No Else Blocks**: Avoid using `else` blocks in conditional statements
- **Early Returns**: Use early returns to handle error cases or special conditions first
- **Guard Clauses**: Use guard clauses to reduce nesting and improve readability
- **Flat Structure**: Prefer flat, linear code flow over nested if-else structures

#### Benefits of Avoiding Else Blocks

- **Readability**: Code reads more linearly from top to bottom
- **Reduced Nesting**: Less indentation makes code easier to understand
- **Early Exit**: Error cases are handled immediately, reducing cognitive load
- **Testability**: Each branch is easier to test independently
- **Maintainability**: Changes to one branch don't affect others as much

#### Correct Pattern - Early Return

**Correct - Early return:**

```go
func (s *SyncOptions) syncFile(ctx context.Context, client ISloClient, loader *FileLoader, filePath string) SyncResult {
    // ... setup code ...

    if s.DryRun {
        // Handle dry-run case
        return result
    }

    // Handle normal case
    response, httpResp, err := client.CreateOrUpdate(ctx, *localSpec)
    if err != nil {
        result.Error = err
        return result
    }

    // Continue with success case
    return result
}
```

**Incorrect - Else block:**

```go
// ❌ Don't do this
if s.DryRun {
    // Handle dry-run case
} else {
    // Handle normal case
}

// ✅ Do this instead
if s.DryRun {
    // Handle dry-run case
    return result
}

// Handle normal case
```

#### When Else Blocks Are Acceptable

The following cases may use `else` blocks:

- **Simple ternary-like logic**: When the else block is very short (1-2 lines) and both branches are equally important
- **Mutually exclusive conditions**: When the conditions are truly mutually exclusive and the else improves clarity
- **Switch statements**: `else` in switch statements is acceptable when it serves as a default case

However, even in these cases, consider if early returns or guard clauses would be clearer.

#### Anti-Patterns to Avoid

- ❌ Using `else` blocks when early returns would be clearer
- ❌ Deeply nested if-else structures
- ❌ `else` blocks that duplicate logic from the `if` block
- ❌ `else` blocks that are significantly longer than the `if` block

#### Examples

- **Correct pattern**: See [`pkg/slocmd/sync.go`](pkg/slocmd/sync.go) - `syncFile()` method uses early returns instead of else blocks
- **Guard clauses**: Use early returns for error handling and special cases before the main logic

### 5. Export Only When Necessary (MANDATORY)

**All constants, variables, types, and functions MUST only be exported if they are used outside the package.**

#### Rules

- **Export Only When Needed**: Only export constants, variables, types, and functions if they are used by other packages
- **Minimal Public API**: Keep the public API surface as small as possible
- **Encapsulation**: Unexported (lowercase) identifiers provide better encapsulation and reduce coupling
- **Check Usage**: Before exporting, verify that the identifier is actually used outside the package
- **Review Periodically**: Periodically review exported identifiers to ensure they are still necessary

#### Benefits of Minimal Exports

- **Encapsulation**: Reduces coupling between packages
- **Flexibility**: Allows internal implementation changes without affecting other packages
- **Clearer API**: Smaller public API makes it easier to understand package boundaries
- **Prevents Misuse**: Prevents other packages from depending on internal implementation details

#### When to Export

- **Export**: Constants, types, or functions that are part of the package's public API
- **Export**: Identifiers that are used by other packages (verified through code search)
- **Export**: Constructors and factory functions (e.g., `NewX()`)
- **Don't Export**: Internal constants used only within the package
- **Don't Export**: Helper functions that are only used internally
- **Don't Export**: Types that are only used as internal implementation details

#### Correct Pattern - Minimal Exports

**Correct - Export only what's needed:**

```go
package mypackage

// Exported: Used by other packages
const PublicConstant = "value"

// Unexported: Only used internally
const internalConstant = "value"

// Exported: Used by other packages
type PublicType struct {
    Field string
}

// Unexported: Only used internally
type internalType struct {
    field string
}

// Exported: Public API function
func NewPublicType() *PublicType {
    return &PublicType{}
}

// Unexported: Internal helper function
func internalHelper() {
    // implementation
}
```

**Incorrect - Over-exporting:**

```go
// ❌ Don't do this - exporting constants that are only used internally
package mypackage

const DefaultValue = 10  // Only used within this package
const Threshold = 5.0     // Only used within this package

// ✅ Do this instead - make them unexported
package mypackage

const defaultValue = 10  // Unexported since only used internally
const threshold = 5.0     // Unexported since only used internally
```

#### Verification Process

Before exporting a constant, variable, type, or function:

1. **Search for Usage**: Use `grep` or code search to find all references to the identifier
2. **Check Package Boundaries**: Verify if references are within the same package or in other packages
3. **Export Only if External**: Only export if the identifier is used in other packages
4. **Document if Public**: If exporting, ensure proper documentation (godoc comments)

#### Anti-Patterns to Avoid

- ❌ Exporting constants that are only used within the same package
- ❌ Exporting helper functions that are internal implementation details
- ❌ Exporting types that are only used internally
- ❌ Exporting "just in case" - only export when there's actual external usage
- ❌ Not reviewing exports periodically to remove unnecessary ones
- ❌ Assuming constants need to be exported for testing (use test helpers instead)

#### Examples

- **Correct pattern**: See [`pkg/slocmd/burn-rate.go`](pkg/slocmd/burn-rate.go) - internal constants are unexported (lowercase) since they're only used within the package
- **Correct pattern**: See [`pkg/service/grafana/types.go`](pkg/service/grafana/types.go) - `RuleActionCreated` and `RuleActionUpdated` are exported because they're used in `pkg/service/slo.go`

### 6. Parallel Operations with errGroup (MANDATORY)

**All parallel operations that need coordinated execution and error handling MUST use `golang.org/x/sync/errgroup`.**

#### Rules

- **Use errGroup for Parallel Operations**: When performing multiple independent operations in parallel (e.g., multiple API calls, database queries), use `errgroup` instead of manual goroutine management
- **Thread-Safe Shared State**: Use `sync.Mutex` or channels to protect shared state when multiple goroutines need to write to the same slice, map, or other data structures
- **Proper Variable Capture**: Always capture loop variables in closures to avoid race conditions
- **Context Propagation**: Use the context from `errgroup.WithContext(ctx)` for all operations to ensure proper cancellation
- **Error Handling**: Handle errors appropriately - return `nil` from goroutines if you want to continue processing even when some operations fail (with logging), or return errors to fail fast

#### Benefits of errGroup

- **Coordinated Execution**: Automatically waits for all goroutines to complete
- **Context Cancellation**: Provides context cancellation that propagates to all goroutines
- **Error Aggregation**: Collects errors from all goroutines
- **Clean Code**: Eliminates manual `sync.WaitGroup` management
- **Better Resource Management**: Ensures proper cleanup and cancellation

#### Correct Pattern - Parallel Operations

**Correct - Using errGroup with proper synchronization:**

```go
import (
    "sync"
    "golang.org/x/sync/errgroup"
)

func (s *Service) ProcessItems(ctx context.Context, items []Item) error {
    var results []Result
    var mu sync.Mutex

    g, gctx := errgroup.WithContext(ctx)
    for _, item := range items {
        // Capture loop variable
        item := item

        g.Go(func() error {
            result, err := s.processItem(gctx, item)
            if err != nil {
                logr.Warn("Failed to process item", "item", item.ID, "error", err)
                // Continue with other items even if one fails
                return nil
            }

            mu.Lock()
            results = append(results, result)
            mu.Unlock()

            return nil
        })
    }

    // Wait for all goroutines to complete
    if err := g.Wait(); err != nil {
        return fmt.Errorf("failed to process items: %w", err)
    }

    return nil
}
```

**Incorrect - Manual goroutine management:**

```go
// ❌ Don't do this
func (s *Service) ProcessItems(ctx context.Context, items []Item) error {
    var results []Result
    var wg sync.WaitGroup

    for _, item := range items {
        wg.Add(1)
        go func(it Item) {
            defer wg.Done()
            result, err := s.processItem(ctx, it)
            if err != nil {
                return
            }
            results = append(results, result) // Race condition!
        }(item)
    }

    wg.Wait()
    return nil
}

// ✅ Do this instead - use errGroup with mutex
func (s *Service) ProcessItems(ctx context.Context, items []Item) error {
    var results []Result
    var mu sync.Mutex

    g, gctx := errgroup.WithContext(ctx)
    for _, item := range items {
        item := item // Capture loop variable
        g.Go(func() error {
            result, err := s.processItem(gctx, item)
            if err != nil {
                return nil // Continue on error
            }
            mu.Lock()
            results = append(results, result)
            mu.Unlock()
            return nil
        })
    }

    return g.Wait()
}
```

#### Variable Capture in Closures

**Critical**: Always capture loop variables explicitly to avoid race conditions:

```go
// ❌ Don't do this - race condition!
for _, item := range items {
    g.Go(func() error {
        return process(item) // item may change before goroutine executes!
    })
}

// ✅ Do this instead - capture the variable
for _, item := range items {
    item := item // Explicitly capture
    g.Go(func() error {
        return process(item) // Safe - uses captured value
    })
}
```

#### Error Handling Patterns

**Continue on errors (with logging):**

```go
g.Go(func() error {
    result, err := operation(gctx)
    if err != nil {
        logr.Warn("Operation failed", "error", err)
        return nil // Continue processing other operations
    }
    // ... process result ...
    return nil
})
```

**Fail fast on errors:**

```go
g.Go(func() error {
    result, err := operation(gctx)
    if err != nil {
        return fmt.Errorf("operation failed: %w", err)
    }
    // ... process result ...
    return nil
})
```

#### Anti-Patterns to Avoid

- ❌ Using manual `sync.WaitGroup` when `errgroup` would be more appropriate
- ❌ Not protecting shared state with mutexes when multiple goroutines write to the same data structure
- ❌ Not capturing loop variables in closures (causes race conditions)
- ❌ Using the original context instead of the context from `errgroup.WithContext`
- ❌ Not handling errors properly in goroutines
- ❌ Accessing shared state without proper synchronization

#### Examples

- **Correct pattern**: See [`pkg/service/slo.go`](pkg/service/slo.go) (lines 511-561) - `GetSloHistory` method uses errgroup to parallelize SLO status range queries with proper mutex protection and variable capture

## Domain Driven Design

### 1. Domain Driven Design Principles (MANDATORY)

**All code MUST follow Domain Driven Design (DDD) principles** to ensure clear domain boundaries, separation of concerns, and maintainable architecture.

#### Rules

- **Domain Models**: Core business logic and entities must be defined in domain models, not in infrastructure or application layers
- **Bounded Contexts**: Organize code into clear bounded contexts that represent distinct business domains
- **Layered Architecture**: Maintain clear separation between:
  - **Domain Layer**: Core business logic, entities, value objects, and domain services
  - **Application Layer**: Use cases, application services, and orchestration logic
  - **Infrastructure Layer**: External concerns (database, APIs, file systems)
  - **Presentation Layer**: UI components and user interfaces
- **Repository Pattern**: Use repositories to abstract data access from domain logic
- **Domain Services**: Extract complex business logic that doesn't naturally fit into entities into domain services
- **Value Objects**: Use value objects for immutable domain concepts that are defined by their attributes
- **Aggregates**: Group related entities into aggregates with clear aggregate roots
- **Dependency Direction**: Dependencies must flow inward - outer layers depend on inner layers, never the reverse

#### Directory Structure

- **Domain Models**: Place domain models in `pkg/repository/repositorymodel/` or domain-specific model directories
- **Repositories**: Define repository interfaces in `pkg/repository/`, implementations in the same package
- **Services**: Application services in `pkg/service/`, domain services in domain layer
- **Bounded Contexts**: Organize code by domain boundaries, not technical layers

#### DDD Implementation Examples

- **Domain Models**: See [`pkg/repository/repositorymodel/`](pkg/repository/repositorymodel/) for domain model definitions
- **Repository Interfaces**: See [`pkg/repository/`](pkg/repository/) for repository interface definitions following DDD patterns
- **Service Layer**: See [`pkg/service/`](pkg/service/) for application services that orchestrate domain logic
- **Layered Architecture**: Review the separation between `pkg/repository/repositorymodel/` (domain), `pkg/service/` (application), and `pkg/repository/` (infrastructure)

#### Anti-Patterns to Avoid

- ❌ Business logic in infrastructure layer (database, HTTP handlers)
- ❌ Domain models depending on infrastructure concerns
- ❌ Anemic domain models (data containers without behavior)
- ❌ Mixing concerns across layers
- ❌ Direct database access from domain layer
- ❌ Circular dependencies between layers

## Code Quality Standards

### 1. No Lint Errors (MANDATORY)

**All code MUST pass linting checks with zero errors.**

#### Rules

- All code must pass linting without any errors
- Warnings should be addressed when possible, but are not blocking
- Run linting checks before committing code
- Fix all lint errors before opening pull requests
- Do not disable lint rules unless absolutely necessary and with proper justification

#### Linting Tools

- **Golang**: `golangci-lint` or standard `go vet` and `go fmt`
- Run linting as part of the development workflow

#### Linting Configuration Examples

- **Pre-commit checks**: Ensure linting is part of your development workflow

### 2. Logging Standards (MANDATORY)

**All code MUST include logs at critical execution points and when interacting with 3rd party libraries.**

#### Rules

- **Critical Points**: Add logs at the start and end of major operations (e.g., method/function invocation, server startup/shutdown).
- **3rd Party Interactions**: Add logs when initializing or interacting with external services/libraries (e.g., database connections, API clients, SCM tools).
- **Log Levels**: Use appropriate log levels (INFO for significant events, DEBUG for detailed tracing, WARN/ERROR for failures).
- **Structured Logging**: Use structured logging (key-value pairs) to provide context.

#### Examples

```go
logger.Info("Starting service", "port", 8080)
logger.Debug("Initializing database connection", "host", "localhost")
if err != nil {
    logger.Error("Failed to connect to database", "error", err)
}
```

### 3. Blind Spot Analysis (MANDATORY)

**All code changes MUST include a blind spot analysis to identify potential issues, edge cases, and missing scenarios.**

#### Rules

- **Systematic Review**: Before completing any code change, systematically review for blind spots
- **Edge Cases**: Identify and handle edge cases, boundary conditions, and error scenarios
- **Missing Scenarios**: Consider what scenarios might be missing from the implementation
- **Error Handling**: Ensure proper error handling for all failure modes
- **Security Considerations**: Review for potential security vulnerabilities or data exposure
- **Performance Impact**: Consider performance implications, especially for user-facing code
- **Integration Points**: Review integration points with other systems or components
- **Data Validation**: Ensure all inputs are validated and sanitized
- **Concurrency Issues**: For concurrent code, identify potential race conditions or deadlocks
- **Resource Management**: Check for proper resource cleanup (file handles, connections, memory)

#### Blind Spot Checklist

When reviewing code, consider:

- ✅ Are all error paths handled?
- ✅ Are edge cases and boundary conditions tested?
- ✅ Are null/undefined values handled appropriately?
- ✅ Are there any security vulnerabilities (SQL injection, XSS, etc.)?
- ✅ Is input validation performed at all entry points?
- ✅ Are resources properly cleaned up?
- ✅ Are there any race conditions or concurrency issues?
- ✅ Are error messages informative but not exposing sensitive information?
- ✅ Are there any performance bottlenecks?
- ✅ Are integration points properly tested?
- ✅ Are there any missing test scenarios?
- ✅ Are there any assumptions that might not hold?

#### Blind Spot Analysis Examples

- **Error Handling**: Review error handling patterns in [`pkg/service/`](pkg/service/) for comprehensive error handling
- **Edge Cases**: Review test files for edge case coverage (e.g., [`pkg/service/slo_test.go`](pkg/service/slo_test.go))
- **Input Validation**: Review validation logic in domain models and services

### 3. Code Documentation (MANDATORY)

**All functions, methods, and exported types MUST have godoc comments that explain what the function does and why it exists.**

#### Rules

- **Godoc Comments**: All exported functions, methods, types, and constants must have godoc comments
- **Private Methods**: Private (unexported) methods should also have comments explaining their purpose
- **Purpose Documentation**: Each godoc comment must explain:
  1. **What** the function does (its behavior)
  2. **Why** the function exists (its purpose and rationale)
  3. **What happens if it doesn't exist** (the impact or consequences of not having this function)
- **Format**: Use standard godoc format starting with the function name or a descriptive phrase
- **Completeness**: Documentation should be sufficient for other developers to understand when and why to use the function
- **Context**: Include relevant context about when the function should be used and any important constraints or side effects

#### Godoc Comment Format

```go
// FunctionName does something specific and explains why it exists.
// This function exists to handle a specific use case or solve a particular problem.
// Without this function, [describe what would happen or what problem would occur].
// Additional context about when to use it, constraints, or important behavior.
func FunctionName() {
    // implementation
}
```

#### Documentation Requirements

- **Exported Functions**: Must have godoc comments starting with the function name
- **Methods**: Must have godoc comments explaining the method's purpose
- **Types**: Must have godoc comments explaining the type's purpose and usage
- **Constants**: Should have comments explaining their purpose and when to use them
- **Complex Logic**: Functions with complex logic should have more detailed documentation
- **Business Logic**: Domain functions should explain the business rationale

#### Documentation Examples

- **Well-documented functions**: See [`pkg/repository/slo.go`](pkg/repository/slo.go) for examples of comprehensive godoc comments that explain what, why, and the impact if the function doesn't exist
- **Method documentation**: Review method documentation in [`pkg/service/slo.go`](pkg/service/slo.go) for examples of purpose-driven documentation that includes impact analysis

#### Anti-Patterns to Avoid

- ❌ Missing godoc comments on exported functions
- ❌ Comments that only describe "what" without explaining "why"
- ❌ Missing explanation of what happens if the function doesn't exist
- ❌ Copying implementation details instead of explaining purpose
- ❌ Vague or generic comments that don't provide useful information
- ❌ Comments that are outdated or don't match the implementation
- ❌ Missing documentation on private methods that have complex logic

### 4. Error Wrapping Standards (MANDATORY)

**All errors MUST be wrapped with `fmt.Errorf` when returning them, and log statements should NOT be used in error return paths.**

#### Rules

- **Wrap Errors**: Always wrap errors with `fmt.Errorf("descriptive message: %w", err)` when returning them
- **No Logging in Error Paths**: Do not log errors in `if err != nil` blocks that immediately return the error
- **Error Context**: Provide descriptive context in the error message explaining what operation failed
- **Error Chain Preservation**: Use `%w` verb to preserve the error chain for error unwrapping

#### Benefits of Error Wrapping

- **Error Context**: Wrapped errors provide context about what operation failed
- **Error Chain**: Preserves the original error for debugging while adding context
- **No Duplicate Logging**: Avoids logging errors that will be logged/handled by callers
- **Consistent Error Handling**: Callers can unwrap errors if needed
- **Better Debugging**: Error messages are more informative

#### Correct Pattern - Error Wrapping

**Correct - Wrap error with context:**

```go
func (s *Service) SomeMethod(ctx context.Context) error {
    result, err := someOperation()
    if err != nil {
        return fmt.Errorf("failed to perform some operation: %w", err)
    }
    return nil
}
```

**Incorrect - Log then return unwrapped error:**

```go
// ❌ Don't do this
func (s *Service) SomeMethod(ctx context.Context) error {
    logr := backend.Logger.FromContext(ctx)
    result, err := someOperation()
    if err != nil {
        logr.Error("Failed to perform operation", "error", err)
        return err  // Error is not wrapped, context is lost
    }
    return nil
}

// ✅ Do this instead
func (s *Service) SomeMethod(ctx context.Context) error {
    result, err := someOperation()
    if err != nil {
        return fmt.Errorf("failed to perform some operation: %w", err)
    }
    return nil
}
```

#### When Logging Errors Is Acceptable

The following cases may log errors:

- **Non-fatal errors**: When an error occurs but execution continues (e.g., background cleanup operations)
- **Error handling**: When you're handling the error (e.g., retrying, falling back) and not immediately returning it
- **Audit logging**: When logging is required for compliance or audit purposes (but still wrap the error)

However, even in these cases, consider if the error should still be wrapped if it's eventually returned.

#### Error Message Guidelines

- **Be Descriptive**: Explain what operation failed, not just that it failed
- **Include Context**: Include relevant identifiers (IDs, names, etc.) when helpful
- **Be Concise**: Keep error messages concise but informative
- **Use Consistent Format**: Use consistent error message format across the codebase

#### Anti-Patterns to Avoid

- ❌ Logging errors in `if err != nil` blocks that immediately return the error
- ❌ Returning unwrapped errors without context
- ❌ Using `fmt.Errorf` without the `%w` verb (loses error chain)
- ❌ Logging errors that will be logged again by callers
- ❌ Vague error messages that don't explain what failed

#### Examples

- **Correct pattern**: See [`pkg/service/slo.go`](pkg/service/slo.go) - errors are wrapped with `fmt.Errorf` and context
- **Error wrapping**: Always use `fmt.Errorf("operation failed: %w", err)` instead of logging and returning unwrapped errors

### 6. Logging Standards (MANDATORY)

**All logging MUST use `backend.Logger.FromContext(ctx)` to ensure proper context propagation and structured logging.**

#### Rules

- **Context-Based Logging**: Always use `backend.Logger.FromContext(ctx)` instead of direct `backend.Logger` calls when a context is available
- **Structured Logging**: Use structured logging with key-value pairs for better log analysis
- **Function Context**: Include function name in logger context using `.With("fn", "FunctionName")` for better traceability
- **Error Logging**: Log errors with appropriate context and details
- **Info Logging**: Log important operations, state changes, and successful completions
- **Log Levels**: Use appropriate log levels:
  - `Info`: Normal operations, successful completions, important state changes
  - `Error`: Errors, failures, exceptions
  - `Warn`: Warning conditions, unexpected but recoverable situations
  - `Debug`: Detailed diagnostic information (if needed)

#### Logging Pattern

All functions with context should follow this pattern:

```go
func (s *Service) SomeMethod(ctx context.Context, param string) error {
    logr := backend.Logger.FromContext(ctx).With("fn", "SomeMethod", "param", param)
    logr.Info("SomeMethod called")

    // ... operation logic ...

    if err != nil {
        logr.Error("Operation failed", "error", err)
        return err
    }

    logr.Info("Operation completed successfully")
    return nil
}
```

#### Logging Requirements

- **Entry Points**: Log at function entry points with relevant parameters
- **Error Paths**: Log all error paths with error details and context
- **Success Paths**: Log successful completion of important operations
- **State Changes**: Log important state changes (create, update, delete operations)
- **Context Fields**: Include relevant context fields (IDs, service names, etc.) in logger context

#### Logging Examples

- **Service layer logging**: See [`pkg/service/slo.go`](pkg/service/slo.go) for examples of context-based logging with structured fields
- **Repository layer logging**: See [`pkg/repository/slo.go`](pkg/repository/slo.go) for database operation logging patterns
- **Error handling logging**: See [`pkg/plugin/errhandler.go`](pkg/plugin/errhandler.go) for error logging patterns

#### Anti-Patterns to Avoid

- ❌ Using `backend.Logger` directly instead of `backend.Logger.FromContext(ctx)` when context is available
- ❌ Missing function name in logger context
- ❌ Not logging errors or important operations
- ❌ Logging sensitive information (passwords, tokens, etc.)
- ❌ Excessive logging that clutters logs
- ❌ Missing context fields that would help debug issues
- ❌ Using string concatenation instead of structured logging key-value pairs

## Defensive Coding Standards

### 1. Avoid Redundant Nil/Null Checks (MANDATORY)

**Do not check for `nil` (Go) or `null`/`undefined` (TS) if a previous check or the type system already guarantees the value exists.**

#### Rules

- **Trust Error Returns**: In Go, if a function returns `(Result, error)` and you have checked `if err != nil`, do not check `if result == nil` unless the function's documentation explicitly states it can return `nil` on success.
- **Trust Type System**: In TypeScript, avoid optional chaining (`?.`) or nullish coalescing (`??`) if the type definitions (or a previous guard clause) guarantee the property is present.
- **Single Source of Truth**: Perform validation once at the boundaries and trust the data thereafter.
- **Trust Constructor Guarantees**: In Go, if a struct field is set by the constructor and the constructor returns a valid instance, do not nil-check that field in methods. The constructor's contract guarantees the field is initialized.

#### Examples

**Correct (Go):**
```go
slo, err := ParseToSLO()
if err != nil {
    return err
}
// Trust slo is not nil here because ParseToSLO guarantees it on success
return slo.Name
```

**Incorrect (Go):**
```go
slo, err := ParseToSLO()
if err != nil {
    return err
}
if slo == nil { // ❌ Redundant: ParseToSLO already handles this
    return errors.New("SLO is nil")
}
```

**Correct (Go - Constructor Guarantee):**
```go
// Constructor always sets grafanaClient
func NewService(client *Client) *Service {
    return &Service{grafanaClient: client}
}

// Method trusts constructor guarantee
func (s *Service) DoWork(ctx context.Context) error {
    return s.grafanaClient.Call(ctx) // ✅ No nil check needed
}
```

**Incorrect (Go - Redundant nil check):**
```go
func (s *Service) DoWork(ctx context.Context) error {
    if s.grafanaClient == nil { // ❌ Redundant: constructor guarantees this
        return errors.New("client not available")
    }
    return s.grafanaClient.Call(ctx)
}
```

### 2. Simplify Nested Property Access (MANDATORY)

**Avoid deeply nested `if` blocks or repeated type assertions for navigating complex structures like JSON/YAML.**

#### Rules

- **Use Helpers**: For Go, use helper functions or libraries to navigate nested maps instead of multiple manual type assertions.
- **Flatten Logic**: Use guard clauses (early returns) instead of nested `if` statements.
- **Destructure in TS**: Use object destructuring in TypeScript to access multiple properties at once.

#### Examples

**Correct (Go - using a helper):**
```go
val := getNestedString(specMap, "spec", "objectives", 0, "ratioMetrics", "good", "source")
if val == "" {
    return defaultVal
}
```

**Incorrect (Go - nested assertions):**
```go
spec, ok := specMap["spec"].(map[string]interface{})
if ok {
    objectives, ok := spec["objectives"].([]interface{})
    if ok && len(objectives) > 0 {
        // ... and so on ... ❌ Too verbose
    }
}
```

### 3. Avoid Redundant Operations (MANDATORY)

**Do not perform the same expensive operation (like unmarshaling or API calls) multiple times in a single flow.**

#### Rules

- **Cache Results**: If you need data from a YAML spec multiple times, unmarshal it once and pass the resulting map/struct.
- **Batch Requests**: Consolidate API calls if possible.

## Documentation Standards

### 1. Code Documentation (MANDATORY)

**All functions, methods, and exported types MUST have godoc comments that explain what the function does and why it exists.**

#### Rules

- **Godoc Comments**: All exported functions, methods, types, and constants must have godoc comments
- **Private Methods**: Private (unexported) methods should also have comments explaining their purpose
- **Purpose Documentation**: Each godoc comment must explain:
  1. **What** the function does (its behavior)
  2. **Why** the function exists (its purpose and rationale)
  3. **What happens if it doesn't exist** (the impact or consequences of not having this function)
- **Format**: Use standard godoc format starting with the function name or a descriptive phrase
- **Completeness**: Documentation should be sufficient for other developers to understand when and why to use the function
- **Context**: Include relevant context about when the function should be used and any important constraints or side effects

### 2. GH-Pages Configuration Documentation (MANDATORY)

**When any configuration struct or option changes, the gh-pages documentation MUST be updated to stay in sync.**

#### Rules

- **Config Reference**: When adding, removing, or modifying fields in any config struct under `pkg/config/`, `pkg/mcp/`, `pkg/messenger/`, `pkg/memory/vector/`, `pkg/tools/secops/`, `pkg/tools/websearch/`, `pkg/iacgen/generator/`, or `pkg/expert/modelprovider/`, the following files must be updated:
  1. `docs/gh-pages/docs.html` — Update the relevant config reference section with the new field, its type, default value, and a human-friendly description
  2. `docs/gh-pages/js/config-builder.js` — Update the form state, section renderer, TOML serializer, and YAML serializer to include the new field with a non-technical tooltip
  3. `config.toml.example` — Add or update the example entry with an inline comment
- **Tooltips**: Every form field in the Config Builder must have a tooltip (the last argument to `fieldText`, `fieldNumber`, `fieldSelect`, `fieldToggle`, or `fieldEnvVar`). Tooltips must be non-technical and explain what the setting does in plain language
- **Docker Examples**: When changing how Genie runs (new flags, entrypoints, volume requirements), update the Docker use cases section in `docs/gh-pages/docs.html`

#### Godoc Comment Format

```go
// FunctionName does something specific and explains why it exists.
// This function exists to handle a specific use case or solve a particular problem.
// Without this function, [describe what would happen or what problem would occur].
// Additional context about when to use it, constraints, or important behavior.
func FunctionName() {
    // implementation
}
```

#### Documentation Requirements

- **Exported Functions**: Must have godoc comments starting with the function name
- **Methods**: Must have godoc comments explaining the method's purpose
- **Types**: Must have godoc comments explaining the type's purpose and usage
- **Constants**: Should have comments explaining their purpose and when to use them
- **Complex Logic**: Functions with complex logic should have more detailed documentation
- **Business Logic**: Domain functions should explain the business rationale

#### Documentation Examples

- **Well-documented functions**: See [`pkg/repository/slo.go`](pkg/repository/slo.go) for examples of comprehensive godoc comments that explain what, why, and the impact if the function doesn't exist
- **Method documentation**: Review method documentation in [`pkg/service/slo.go`](pkg/service/slo.go) for examples of purpose-driven documentation that includes impact analysis

#### Anti-Patterns to Avoid

- ❌ Missing godoc comments on exported functions
- ❌ Comments that only describe "what" without explaining "why"
- ❌ Missing explanation of what happens if the function doesn't exist
- ❌ Copying implementation details instead of explaining purpose
- ❌ Vague or generic comments that don't provide useful information
- ❌ Comments that are outdated or don't match the implementation
- ❌ Missing documentation on private methods that have complex logic

## Testing Requirements

### General Testing Rules

- **No Redundant Tests (MANDATORY)**: THERE MUST be no redundant tests. Each test should cover unique functionality or scenarios. Avoid duplicating test coverage across different test files or test suites.

### Golang Testing

- **Framework (MANDATORY)**: Use Ginkgo and Gomega for all Golang tests
- **BDD Style (MANDATORY)**: All unit tests MUST use BDD (Behavior-Driven Development) style with `Describe`, `Context`, and `It` blocks
- **Context from It Blocks (MANDATORY)**: All `It` blocks that need a context MUST receive `ctx context.Context` as a parameter from the `It` block. Never use `context.Background()` directly in tests. The context MUST be passed from the `It` block to ensure proper context propagation and test lifecycle management.
- **Test Structure**: Use Ginkgo's BDD-style test structure with `Describe`, `Context`, and `It` blocks
- **Table-Driven Tests (RECOMMENDED)**: Use `DescribeTable` for testing multiple scenarios with similar logic to improve readability and maintainability
- **Assertions**: Use Gomega matchers for all assertions (e.g., `Expect().To()`, `Expect().Should()`)
- **Mocks**: Use counterfeiter-generated fakes
- **Coverage**: Run `mage coverage` to check test coverage
- **Test Location**: `*_test.go` files alongside source files
- **Test Suite**: Each package should have a test suite file (e.g., `logexplorer_suite_test.go`) that sets up Ginkgo/Gomega

#### BDD Unit Test Requirements

**All unit tests MUST follow BDD principles:**

- **Given-When-Then Structure**: Structure tests to clearly express the scenario (Given), action (When), and expected outcome (Then)
- **Descriptive Test Names**: Use `Describe` and `It` blocks with descriptive names that explain the behavior being tested
- **Context Blocks**: Use `Context` blocks to group related scenarios and express different conditions
- **Behavior-Focused**: Focus on testing behavior and outcomes, not implementation details
- **Readable Scenarios**: Write tests that read like specifications, making it clear what the code should do

#### BDD Test Naming Conventions

- **Describe blocks**: Describe the feature or function being tested (e.g., `Describe("ProcessLogEntry", ...)`)
- **Context blocks**: Describe the condition or scenario (e.g., `Context("when the entry is valid", ...)`)
- **It blocks**: Describe the expected behavior (e.g., `It("should parse the log entry correctly", ...)`)

#### When to Use BDD

- **Unit Tests**: Always use BDD style for unit tests
- **Integration Tests**: Use BDD style for integration tests when testing behavior
- **Complex Scenarios**: Especially valuable for complex scenarios with multiple conditions
- **Edge Cases**: Use BDD to clearly express edge cases and boundary conditions

#### Context Usage in It Blocks (MANDATORY)

**All `It` blocks that require a context MUST receive `ctx context.Context` as a parameter:**

- **Never use `context.Background()`**: Do not call `context.Background()` directly in test code
- **Never assign context in BeforeEach**: Do not create or assign context variables in `BeforeEach` blocks
- **Always use ctx parameter**: All `It` blocks that need context must have `func(ctx context.Context)` signature
- **Pass ctx to functions**: Always pass the `ctx` parameter to functions that require context

**Correct Pattern:**

```go
var _ = Describe("FeatureName", func() {
    Context("when condition", func() {
        It("should behave correctly", func(ctx context.Context) {
            result := SomeFunction(ctx)
            Expect(result).To(Equal(expectedValue))
        })
    })
})
```

**Incorrect Patterns:**

```go
// ❌ Don't do this - using context.Background() directly
It("should behave correctly", func() {
    result := SomeFunction(context.Background())
    Expect(result).To(Equal(expectedValue))
})

// ❌ Don't do this - assigning context in BeforeEach
var ctx context.Context
BeforeEach(func() {
    ctx = context.Background()
})
It("should behave correctly", func() {
    result := SomeFunction(ctx)
    Expect(result).To(Equal(expectedValue))
})

// ✅ Do this instead - use ctx from It block
It("should behave correctly", func(ctx context.Context) {
    result := SomeFunction(ctx)
    Expect(result).To(Equal(expectedValue))
})
```

#### Ginkgo/Gomega Test Structure

```go
package mypackage_test

import (
    "context"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

var _ = Describe("FeatureName", func() {
    Context("when condition", func() {
        It("should behave correctly", func(ctx context.Context) {
            result := SomeFunction(ctx)
            Expect(result).To(Equal(expectedValue))
        })
    })
})
```

#### Table-Driven Tests with DescribeTable

When testing multiple scenarios with similar logic, use `DescribeTable` for better readability:

```go
var _ = Describe("FormatValidationError", func() {
    DescribeTable("should handle different error types correctly",
        func(err error, expectedSubstrings []string) {
            result := FormatValidationError(err)
            for _, expected := range expectedSubstrings {
                Expect(result).To(ContainSubstring(expected))
            }
        },
        Entry("nil error", nil, []string{}),
        Entry("simple error", errors.New("test"), []string{"test"}),
        Entry("wrapped error", fmt.Errorf("wrapped: %w", errors.New("base")), []string{"wrapped", "base"}),
    )
})
```

#### Golang Testing Examples

- **Ginkgo Test Suite**: See [`pkg/service/init_test.go`](pkg/service/init_test.go) for test suite setup
- **Ginkgo Tests**: See [`pkg/service/slo_test.go`](pkg/service/slo_test.go) for comprehensive Ginkgo/Gomega test examples with BDD style
- **Context Usage in Tests**: See [`pkg/slocmd/list_test.go`](pkg/slocmd/list_test.go) for examples of using `ctx context.Context` parameter in `It` blocks
- **Repository Test Suite**: See [`pkg/repository/init_test.go`](pkg/repository/init_test.go) for repository test suite setup

## GitHub Pull Request Workflow

### 1. Addressing Review Comments (MANDATORY)

**When working on PR review comments, follow this workflow:**

#### Fetching Review Comments

Use `gh` CLI to fetch PR review comments and threads:

```bash
# Get all PR comments (includes review comments)
gh api repos/appcd-dev/stackgen-genie/pulls/<PR_NUMBER>/comments

# Get review threads with resolution status using GraphQL
gh api graphql -f query='
  query { 
    repository(owner: "appcd-dev", name: "stackgen-genie") {
      pullRequest(number: <PR_NUMBER>) {
        reviewThreads(first: 50) {
          nodes {
            id
            isResolved
            comments(first: 1) {
              nodes {
                body
              }
            }
          }
        }
      }
    }
  }
'
```

#### Resolving Review Threads

After addressing a review comment with code changes:

1. **Fix the code** as requested in the review comment
2. **Commit and push** the changes
3. **Add a reply comment** to the thread explaining:
   - **What was done** (or what was not done)
   - **Why** it was done this way (or why it was not addressed)
   - **Commit SHA** that contains the fix

```bash
# Add a reply to the review thread before resolving
gh api repos/appcd-dev/stackgen-genie/pulls/<PR_NUMBER>/comments/<COMMENT_ID>/replies \
  -f body="Fixed in commit \`<COMMIT_SHA>\`.

**What:** Added console.warn for debugging in getStorageValue.
**Why:** Matches pattern used in setStorageValue/removeStorageValue for consistency."
```

For comments that are **not addressed**, still add a reply explaining why:

```bash
gh api repos/appcd-dev/stackgen-genie/pulls/<PR_NUMBER>/comments/<COMMENT_ID>/replies \
  -f body="Not addressed in this PR.

**Why:** This is a larger architectural refactor that would be better suited for a separate PR. The current implementation works correctly."
```

4. **Resolve the thread** using the GraphQL mutation:

```bash
gh api graphql -f query='
mutation {
  resolveReviewThread(input: {threadId: "<THREAD_ID>"}) {
    thread { id isResolved }
  }
}'
```

The `<THREAD_ID>` is the `id` field from the review thread query (e.g., `PRRT_kwDOQ0o-5s5rSDDB`).
The `<COMMENT_ID>` is the numeric `id` from the PR comments API (e.g., `2733161868`).

#### Adding Summary Comments

After addressing review comments, add a summary comment to the PR:

```bash
gh pr comment <PR_NUMBER> --repo appcd-dev/stackgen-genie --body "Addressed review comments:

1. **Issue description** - Brief explanation of the fix (commit: \`<SHA>\`)
2. **Another issue** - Brief explanation of the fix (commit: \`<SHA>\`)

**Not addressed:**
- **Issue** - Reason why not addressed (e.g., larger refactor, separate PR)

Verified with tests and browser testing."
```

#### Workflow Summary

1. `gh api` to fetch review comments
2. Fix the issues in code
3. `go test ./...` and `go vet ./...` to verify
4. `git commit` and `git push`
5. `gh api` to add reply comments to each thread (explaining what/why + commit SHA)
6. `gh api graphql` mutation to resolve each addressed thread
7. `gh pr comment` to add summary of changes

## Reference Examples

### Golang Interface Examples

**Complete interface with counterfeiter and tests:**

- **Interface Definition**: [`pkg/repository/slo.go`](pkg/repository/slo.go) (lines 49-54) - `ISloRepo` interface following the 2-parameter pattern
- **Counterfeiter Setup**: [`pkg/repository/fakes.go`](pkg/repository/fakes.go) - Counterfeiter annotations for all interfaces
- **Test with Fakes**: [`pkg/service/slo_test.go`](pkg/service/slo_test.go) - Using counterfeiter-generated fakes in tests with BDD style
- **Generated Fake**: [`pkg/repository/repositoryfakes/fake_islo_repo.go`](pkg/repository/repositoryfakes/fake_islo_repo.go) - Generated fake implementation

## Enforcement

These standards are **mandatory** and must be followed for all new code. When reviewing pull requests:

1. ✅ Verify all Golang interfaces have exactly 2 parameters (ctx + request struct)
2. ✅ Verify all Golang interfaces have counterfeiter annotations
3. ✅ Verify counterfeiter fakes are generated and used in tests
4. ✅ Verify functions are methods on types rather than package-level functions (except constructors)
5. ✅ Verify code avoids else blocks, using early returns or guard clauses instead
6. ✅ Verify constants, variables, types, and functions are only exported if used outside the package
7. ✅ Verify parallel operations use `errgroup` with proper synchronization and variable capture
8. ✅ Verify code follows Domain Driven Design principles with clear layer separation
9. ✅ Verify domain logic is in domain layer, not infrastructure or application layers
10. ✅ Verify there are no redundant tests (each test covers unique functionality)
11. ✅ Verify all code passes linting with zero errors
12. ✅ Verify blind spot analysis has been performed for all code changes (edge cases, error handling, security, etc.)
13. ✅ Verify all functions, methods, and exported types have godoc comments explaining what they do, why they exist, and what happens if they don't exist
14. ✅ Verify all errors are wrapped with `fmt.Errorf` when returned, and log statements are not used in error return paths
15. ✅ Verify all business errors use `problems.New` with appropriate HTTP status codes and error codes
16. ✅ Verify all logging uses `backend.Logger.FromContext(ctx)` when context is available
17. ✅ Verify all Golang unit tests use BDD style with `Describe`, `Context`, and `It` blocks
18. ✅ Verify all `It` blocks that need context receive `ctx context.Context` as a parameter and never use `context.Background()` directly

## Agent Workflow Guidelines

### 1. Handling PR Reviews (MANDATORY)

**Agents MUST follow this workflow when addressing PR review comments to ensure nothing is missed.**

#### Rules

- **Fetch All Comments**: Don't rely on `gh pr view` summary alone. It misses inline comments.
- **Fetch Inline Comments**: Use `gh api` to fetch specific line-item comments.
- **Resolution**: Comments must be resolved via the CLI to close the loop effectively.

#### Workflow

1. **Analysis Phase**:
   - Run `gh pr view <id> --json body,comments` to get top-level context.
   - Run `gh api repos/:owner/:repo/pulls/<id>/comments` to get inline code comments.
   - Create a plan to address **ALL** comments.

2. **Resolution Phase**:
   - After fixing code, fetch unresolved threads to identify what needs closing:
     ```bash
     gh api graphql -f query='query { repository(owner: ":owner", name: ":repo") { pullRequest(number: <id>) { reviewThreads(first: 50) { nodes { id isResolved comments(first: 1) { nodes { body } } } } } } }'
     ```
   - Resolve threads programmatically using the GraphQL API:
     ```bash
     gh api graphql -f query='mutation { resolveReviewThread(input: {threadId: "$id"}) { thread { isResolved } } }'
     ```

## Migration Guide

For existing code that doesn't follow these standards:

1. **Golang Interfaces**: Refactor method signatures to use request structs
2. **Counterfeiter**: Add annotations and generate fakes for existing interfaces
3. **Package-Level Functions**: Refactor package-level functions to be methods on appropriate types
4. **Else Blocks**: Refactor if-else blocks to use early returns or guard clauses
5. **Export Only When Necessary**: Unexport constants, variables, types, and functions that are only used within the package
6. **Parallel Operations**: Refactor manual goroutine management to use `errgroup` with proper synchronization
7. **Domain Driven Design**: Refactor code to follow DDD principles, moving business logic to domain layer
8. **Linting**: Fix all lint errors in existing code before making changes
9. **Blind Spot Analysis**: Perform blind spot analysis for existing code and add missing edge case handling
10. **Code Documentation**: Add godoc comments to all functions, methods, and exported types explaining what they do and why they exist
11. **Error Wrapping**: Replace log-then-return patterns with `fmt.Errorf("context: %w", err)` and remove log statements from error return paths
12. **Business Errors**: Replace `fmt.Errorf` with `problems.New` for all business validation errors, domain rule violations, and user-facing errors
13. **Logging**: Replace direct `backend.Logger` calls with `backend.Logger.FromContext(ctx)` where context is available
14. **BDD Unit Tests**: Refactor existing Golang unit tests to use BDD style with `Describe`, `Context`, and `It` blocks
15. **Context in Tests**: Refactor all Ginkgo tests to use `ctx context.Context` parameter in `It` blocks instead of `context.Background()` or context variables in `BeforeEach`

## Questions or Issues?

If you have questions about these standards or encounter issues implementing them, please:

1. Review the reference examples linked in this document
2. Check the actual implementation files in the repository
3. Open an issue or discussion for clarification

### 7. Configuration Management (MANDATORY)

**Configuration structs MUST be defined in the package that uses them, but loading and aggregation is centralized.**

#### Rules

- **Co-located Config Structs**: Define the configuration struct for a component in the same package as the component (e.g., `pkg/tools/websearch/config.go` or inside `websearch.go`).
- **Aggregated in `pkg/config`**: The centralized `pkg/config` package imports these component packages and aggregates their config structs into the main `GenieConfig`.
- **No `os.Getenv` in Components**: Components must NOT read environment variables directly. They receive their configuration via dependency injection (constructors).
- **Centralized Loading**: `os.Getenv` and file loading logic resides *only* in `pkg/config` (or the application entry point). `pkg/config` populates the component config structs with defaults and environment variables.

#### Benefits

- **Modularity**: Components define their own configuration requirements.
- **Decoupling**: Components don't depend on a central `config` package (avoids circular dependencies).
- **Testability**: Components can be tested with struct literals without setting env vars.
- **Discoverability**: The main `GenieConfig` still provides a view of all application configuration.

#### Example

**Correct:**

```go
// pkg/tools/mytool/tool.go
package mytool

type Config struct {
    APIKey string `yaml:"api_key" toml:"api_key"`
}

func NewTool(cfg Config) *Tool {
    return &Tool{key: cfg.APIKey}
}

// pkg/config/config.go
package config

import "github.com/appcd-dev/genie/pkg/tools/mytool"

type GenieConfig struct {
    MyTool mytool.Config `yaml:"my_tool" toml:"my_tool"`
}

func LoadConfig() GenieConfig {
    // Load from file...
    // Apply defaults/env vars
    cfg.MyTool.APIKey = os.Getenv("MY_TOOL_API_KEY")
    return cfg
}
```

## Acceptance Criteria

### Feature Acceptance Criteria (MANDATORY)

**Whenever a new end-user-facing feature is developed, the [`qa/`](qa/) directory MUST be updated** with a dedicated acceptance test file or section for the feature.

> The `qa/` folder is exclusively for **blackbox acceptance testing** of the Genie web chat. Tests describe what an end user can see, click, or verify through the chat UI or HTTP endpoints — never internal implementation details.

#### Rules

- **Every new end-user-facing feature** must have a corresponding entry in `qa/` before the feature is considered complete
- **Blackbox only**: Validation steps must be executable by a human tester using the chat UI (`docs/gh-pages/chat.html`) or HTTP endpoints (e.g., `curl`). Never reference unit tests, `go test` commands, or internal source paths
- Each entry must cover the following four aspects:
  1. **Why it was developed** — the motivation and context behind the feature
  2. **What problem it solves** — the specific pain point or gap it addresses
  3. **How it is beneficial** — the value it provides to users or the system
  4. **Arrange / Act / Assert** — concrete steps a human tester follows to validate the feature

#### Entry Format

Each feature entry should follow this structure:

```markdown
## Feature Name

### Why
Explain why this feature was developed, including the context or user request that motivated it.

### Problem
Describe the specific problem this feature solves.

### Benefit
Explain how this feature is beneficial to users, the system, or the development workflow.

### Arrange
- Server running (Test 1)
- Connected via `docs/gh-pages/chat.html` (Test 2)

### Act
Describe the user actions to perform (e.g., send a chat message, click a button, call an HTTP endpoint).

### Assert
Describe the expected outcomes visible to the user (e.g., response content, UI indicators, HTTP status codes).
```

#### Anti-Patterns to Avoid

- ❌ Merging a feature without updating `qa/`
- ❌ Writing vague or generic acceptance criteria that don't actually validate the feature
- ❌ Omitting the Arrange/Act/Assert validation steps
- ❌ Skipping the "Why" or "Problem" sections — these provide essential context for future maintainers
- ❌ Using unit test commands (`go test`, `go vet`) as validation steps — `qa/` is for blackbox acceptance tests only
- ❌ Referencing internal source paths (`*_test.go`, `pkg/...`) in validation steps — tests must be executable by someone with no knowledge of the codebase internals
