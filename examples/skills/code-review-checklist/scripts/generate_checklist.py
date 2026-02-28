#!/usr/bin/env python3
"""
Generate code review checklist based on language and framework.
"""
import os
import argparse
from datetime import datetime

CHECKLISTS = {
    "python": {
        "general": [
            "Code follows PEP 8 style guidelines",
            "Variable and function names are descriptive and follow snake_case",
            "Functions are small and focused (< 50 lines recommended)",
            "No commented-out code or debug print statements",
            "Proper use of list comprehensions and generators",
            "Type hints are used for function signatures",
            "Docstrings follow Google or NumPy style",
        ],
        "testing": [
            "Unit tests use pytest or unittest",
            "Test coverage > 80%",
            "Tests are independent and can run in any order",
            "Mock external dependencies appropriately",
            "Test edge cases and error conditions",
        ],
        "security": [
            "No use of eval() or exec() with user input",
            "SQL queries use parameterization (no string formatting)",
            "File operations validate paths (prevent traversal)",
            "Secrets use environment variables, not hardcoded",
            "Dependencies are up to date (check requirements.txt)",
        ],
    },
    "go": {
        "general": [
            "Code follows Go formatting (gofmt)",
            "Variable and function names follow Go conventions",
            "Error handling is explicit (no ignored errors)",
            "Defer used appropriately for cleanup",
            "Interfaces are small and focused",
            "Context used for cancellation and timeouts",
            "No goroutine leaks",
        ],
        "testing": [
            "Tests use table-driven test pattern where appropriate",
            "Test coverage > 80% (go test -cover)",
            "Benchmarks for performance-critical code",
            "Examples for public API functions",
            "Tests clean up resources properly",
        ],
        "security": [
            "Input validation for all external data",
            "SQL queries use prepared statements",
            "Crypto uses crypto/rand, not math/rand",
            "TLS configuration is secure",
            "No hardcoded credentials or secrets",
        ],
    },
    "javascript": {
        "general": [
            "Code follows ESLint rules",
            "Const/let used instead of var",
            "Arrow functions used appropriately",
            "Async/await preferred over callbacks",
            "No console.log in production code",
            "Proper error handling with try/catch",
            "Destructuring used where beneficial",
        ],
        "testing": [
            "Tests use Jest, Mocha, or similar framework",
            "Test coverage > 80%",
            "Async tests handle promises correctly",
            "Mock external APIs and services",
            "Tests are isolated and independent",
        ],
        "security": [
            "Input sanitization prevents XSS",
            "No eval() or Function() with user input",
            "Dependencies checked for vulnerabilities (npm audit)",
            "CORS configured appropriately",
            "Secrets not committed to repository",
        ],
    },
}

FRAMEWORK_SPECIFIC = {
    "django": [
        "Models use appropriate field types and validators",
        "Migrations are included and tested",
        "Views use appropriate mixins/decorators",
        "Templates escape user input ({{ }} not {% autoescape off %})",
        "URLs follow naming conventions",
        "Settings use environment variables for secrets",
        "ORM queries optimized (select_related/prefetch_related)",
    ],
    "react": [
        "Components are functional with hooks",
        "Props are validated with PropTypes or TypeScript",
        "State management is appropriate (useState, useContext, Redux)",
        "useEffect dependencies are correct",
        "No unnecessary re-renders",
        "Accessibility attributes (ARIA) used",
        "Keys used correctly in lists",
    ],
    "gin": [
        "Routes are organized logically",
        "Middleware used for cross-cutting concerns",
        "Request validation uses binding",
        "Errors returned with appropriate HTTP status codes",
        "Context passed to downstream functions",
        "Graceful shutdown implemented",
    ],
}

def generate_checklist(language, framework=None, project_type=None):
    """Generate a code review checklist."""
    checklist = []
    
    # Header
    checklist.append(f"# Code Review Checklist - {language.title()}")
    if framework:
        checklist.append(f"**Framework:** {framework.title()}")
    if project_type:
        checklist.append(f"**Project Type:** {project_type.title()}")
    checklist.append(f"**Generated:** {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    checklist.append("")
    
    # General code quality
    if language.lower() in CHECKLISTS:
        lang_checks = CHECKLISTS[language.lower()]
        
        checklist.append("## General Code Quality")
        for item in lang_checks.get("general", []):
            checklist.append(f"- [ ] {item}")
        checklist.append("")
        
        # Testing
        checklist.append("## Testing")
        for item in lang_checks.get("testing", []):
            checklist.append(f"- [ ] {item}")
        checklist.append("")
        
        # Security
        checklist.append("## Security")
        for item in lang_checks.get("security", []):
            checklist.append(f"- [ ] {item}")
        checklist.append("")
    
    # Framework-specific
    if framework and framework.lower() in FRAMEWORK_SPECIFIC:
        checklist.append(f"## {framework.title()}-Specific")
        for item in FRAMEWORK_SPECIFIC[framework.lower()]:
            checklist.append(f"- [ ] {item}")
        checklist.append("")
    
    # Universal checks
    checklist.append("## Documentation")
    checklist.append("- [ ] Code comments explain 'why', not 'what'")
    checklist.append("- [ ] Public APIs have documentation")
    checklist.append("- [ ] README updated if needed")
    checklist.append("- [ ] CHANGELOG updated")
    checklist.append("")
    
    checklist.append("## Performance")
    checklist.append("- [ ] No obvious performance bottlenecks")
    checklist.append("- [ ] Database queries are optimized")
    checklist.append("- [ ] Appropriate caching strategies")
    checklist.append("- [ ] Resource cleanup (connections, files, etc.)")
    checklist.append("")
    
    return "\n".join(checklist)

def main():
    parser = argparse.ArgumentParser(description="Generate code review checklist")
    parser.add_argument("language", help="Programming language (python, go, javascript, etc.)")
    parser.add_argument("--framework", help="Framework name (django, react, gin, etc.)")
    parser.add_argument("--type", dest="project_type", help="Project type (web, microservice, cli, etc.)")
    
    args = parser.parse_args()
    
    # Generate checklist
    checklist = generate_checklist(args.language, args.framework, args.project_type)
    
    # Write to output directory
    output_dir = os.environ.get("OUTPUT_DIR", "./output")
    os.makedirs(output_dir, exist_ok=True)
    
    output_file = os.path.join(output_dir, "checklist.md")
    with open(output_file, "w") as f:
        f.write(checklist)
    
    print(f"Checklist generated: {output_file}")
    print(f"Language: {args.language}")
    if args.framework:
        print(f"Framework: {args.framework}")
    if args.project_type:
        print(f"Project Type: {args.project_type}")
    print("")
    print(checklist)

if __name__ == "__main__":
    main()
