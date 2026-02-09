---
name: code-review-checklist
description: Generate comprehensive code review checklists tailored to programming language and framework
---

# Code Review Checklist

Generates customized code review checklists based on the programming language, framework, and project type to ensure thorough and consistent code reviews.

## When to Use This Skill

Use this skill when you need to:
- Conduct code reviews with comprehensive checklists
- Ensure consistent review quality across team
- Onboard new reviewers with structured guidance
- Customize review criteria for different languages/frameworks
- Generate PR review templates

## Usage

### Generate Checklist

```bash
python3 scripts/generate_checklist.py <language> [--framework=<name>] [--type=<project-type>]
```

**Examples:**
```bash
# Python web application
python3 scripts/generate_checklist.py python --framework=django --type=web

# Go microservice
python3 scripts/generate_checklist.py go --framework=gin --type=microservice

# JavaScript frontend
python3 scripts/generate_checklist.py javascript --framework=react --type=frontend

# General purpose (no framework)
python3 scripts/generate_checklist.py java
```

## Supported Languages

- **Python**: Django, Flask, FastAPI
- **JavaScript/TypeScript**: React, Vue, Angular, Node.js
- **Go**: Gin, Echo, standard library
- **Java**: Spring Boot, Jakarta EE
- **Ruby**: Rails, Sinatra
- **Rust**: Actix, Rocket
- **C#**: ASP.NET Core

## Checklist Categories

Each generated checklist includes:

### 1. Code Quality
- Readability and maintainability
- Naming conventions
- Code organization
- DRY principle adherence
- SOLID principles (OOP languages)

### 2. Functionality
- Requirements implementation
- Edge case handling
- Error handling
- Input validation
- Business logic correctness

### 3. Testing
- Unit test coverage
- Integration tests
- Test quality and assertions
- Mock usage
- Test naming and organization

### 4. Security
- Input sanitization
- Authentication/authorization
- Sensitive data handling
- SQL injection prevention
- XSS prevention (web apps)
- Dependency vulnerabilities

### 5. Performance
- Algorithm efficiency
- Database query optimization
- Caching strategies
- Resource management
- Memory leaks

### 6. Documentation
- Code comments
- API documentation
- README updates
- Changelog entries
- Inline documentation

### 7. Language-Specific
- Idiomatic code patterns
- Language best practices
- Framework conventions
- Standard library usage

## Output

Generates multiple files in `$OUTPUT_DIR`:
- `checklist.md`: Main review checklist
- `quick_reference.md`: Quick reference guide
- `language_specifics.md`: Language-specific considerations
- `security_checklist.md`: Security-focused checklist

## Example Checklist (Python/Django)

```markdown
# Code Review Checklist - Python/Django

## General Code Quality
- [ ] Code follows PEP 8 style guidelines
- [ ] Variable and function names are descriptive
- [ ] Functions are small and focused (< 50 lines)
- [ ] No commented-out code
- [ ] No print statements (use logging)

## Django-Specific
- [ ] Models use appropriate field types
- [ ] Migrations are included and tested
- [ ] Views use appropriate mixins/decorators
- [ ] Templates escape user input
- [ ] URLs follow naming conventions
- [ ] Settings use environment variables for secrets

## Security
- [ ] User input is validated and sanitized
- [ ] SQL queries use parameterization
- [ ] CSRF protection is enabled
- [ ] Authentication is properly implemented
- [ ] Permissions are checked before operations
- [ ] No hardcoded credentials

## Testing
- [ ] Unit tests for models and business logic
- [ ] Tests for views/endpoints
- [ ] Edge cases are tested
- [ ] Test coverage > 80%
- [ ] Tests are independent and repeatable

## Performance
- [ ] Database queries are optimized (no N+1)
- [ ] select_related/prefetch_related used appropriately
- [ ] Caching implemented where beneficial
- [ ] Large datasets are paginated
```

## Dependencies

- Python 3.8+

## Customization

You can customize checklists by editing templates in the `assets/` directory:
- `assets/python_template.md`
- `assets/javascript_template.md`
- `assets/go_template.md`
- etc.

## Best Practices

### For Reviewers
1. Use the checklist as a guide, not a strict rulebook
2. Focus on high-impact issues first
3. Provide constructive feedback with examples
4. Acknowledge good practices
5. Ask questions rather than making demands

### For Authors
1. Self-review using the checklist before requesting review
2. Address all checklist items proactively
3. Explain any intentional deviations
4. Keep PRs small and focused
5. Respond to feedback promptly
