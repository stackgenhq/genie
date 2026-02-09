#!/bin/bash
# Validate Terraform configuration with comprehensive checks

set -e

TF_DIR="$1"
MODE="${2:-full}"
OUTPUT_DIR="${OUTPUT_DIR:-./output}"

if [ -z "$TF_DIR" ]; then
    echo "Usage: $0 <terraform-directory> [--format-only|--security-only]"
    exit 1
fi

if [ ! -d "$TF_DIR" ]; then
    echo "Error: Directory not found: $TF_DIR"
    exit 1
fi

mkdir -p "$OUTPUT_DIR"
REPORT="$OUTPUT_DIR/validation_report.txt"

echo "Terraform Validation Report" > "$REPORT"
echo "============================" >> "$REPORT"
echo "" >> "$REPORT"
echo "Directory: $TF_DIR" >> "$REPORT"
echo "Generated: $(date)" >> "$REPORT"
echo "" >> "$REPORT"

cd "$TF_DIR"

ERRORS=0
WARNINGS=0

# 1. Format Check
echo "Running terraform fmt check..." | tee -a "$REPORT"
if terraform fmt -check -recursive . >> "$REPORT" 2>&1; then
    echo "✓ PASSED: Terraform format check" | tee -a "$REPORT"
else
    echo "✗ FAILED: Terraform format check" | tee -a "$REPORT"
    echo "  Run 'terraform fmt -recursive' to fix" | tee -a "$REPORT"
    ((ERRORS++))
fi
echo "" >> "$REPORT"

if [ "$MODE" = "--format-only" ]; then
    cat "$REPORT"
    exit $ERRORS
fi

# 2. Initialize (if needed)
if [ ! -d ".terraform" ]; then
    echo "Initializing Terraform..." | tee -a "$REPORT"
    terraform init -backend=false >> "$REPORT" 2>&1 || true
fi

# 3. Validate
echo "Running terraform validate..." | tee -a "$REPORT"
if terraform validate >> "$REPORT" 2>&1; then
    echo "✓ PASSED: Terraform validate" | tee -a "$REPORT"
else
    echo "✗ FAILED: Terraform validate" | tee -a "$REPORT"
    ((ERRORS++))
fi
echo "" >> "$REPORT"

# 4. Security Checks
echo "Running security checks..." | tee -a "$REPORT"
SECURITY_ISSUES=0

# Check for hardcoded credentials
if grep -r "aws_access_key\|aws_secret_key\|password.*=.*\"" --include="*.tf" . 2>/dev/null; then
    echo "✗ CRITICAL: Hardcoded credentials found" | tee -a "$REPORT"
    ((SECURITY_ISSUES++))
    ((ERRORS++))
fi

# Check for overly permissive security groups
if grep -r "0\.0\.0\.0/0" --include="*.tf" . 2>/dev/null | grep -v "#"; then
    echo "✗ HIGH: Overly permissive security group rules (0.0.0.0/0)" | tee -a "$REPORT"
    echo "  Recommendation: Restrict to specific IP ranges" | tee -a "$REPORT"
    ((SECURITY_ISSUES++))
    ((WARNINGS++))
fi

# Check for S3 bucket encryption
if grep -r "aws_s3_bucket" --include="*.tf" . 2>/dev/null | grep -v "server_side_encryption" | grep -q "resource"; then
    echo "⚠ MEDIUM: S3 buckets may be missing encryption" | tee -a "$REPORT"
    echo "  Recommendation: Add server_side_encryption_configuration" | tee -a "$REPORT"
    ((SECURITY_ISSUES++))
    ((WARNINGS++))
fi

# Check for public S3 buckets
if grep -r "acl.*=.*\"public" --include="*.tf" . 2>/dev/null; then
    echo "✗ CRITICAL: Public S3 bucket ACL detected" | tee -a "$REPORT"
    ((SECURITY_ISSUES++))
    ((ERRORS++))
fi

if [ $SECURITY_ISSUES -eq 0 ]; then
    echo "✓ PASSED: Security checks" | tee -a "$REPORT"
else
    echo "Found $SECURITY_ISSUES security issues" | tee -a "$REPORT"
fi
echo "" >> "$REPORT"

if [ "$MODE" = "--security-only" ]; then
    cat "$REPORT"
    exit $ERRORS
fi

# 5. Best Practices
echo "Checking best practices..." | tee -a "$REPORT"

# Check for backend configuration
if ! grep -q "backend " *.tf 2>/dev/null; then
    echo "⚠ WARNING: No backend configuration found" | tee -a "$REPORT"
    echo "  Recommendation: Configure remote state backend" | tee -a "$REPORT"
    ((WARNINGS++))
fi

# Check for variable descriptions
if grep -r "variable " --include="*.tf" . 2>/dev/null | grep -v "description" | grep -q "variable"; then
    echo "⚠ WARNING: Some variables missing descriptions" | tee -a "$REPORT"
    ((WARNINGS++))
fi

# Check for required tags
if grep -r "resource \"aws_" --include="*.tf" . 2>/dev/null | grep -v "tags" | grep -q "resource"; then
    echo "⚠ WARNING: Some resources missing tags" | tee -a "$REPORT"
    echo "  Recommendation: Add tags for Environment, Owner, CostCenter" | tee -a "$REPORT"
    ((WARNINGS++))
fi

echo "" >> "$REPORT"

# Summary
echo "SUMMARY" >> "$REPORT"
echo "=======" >> "$REPORT"
echo "Errors: $ERRORS" >> "$REPORT"
echo "Warnings: $WARNINGS" >> "$REPORT"
echo "" >> "$REPORT"

if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    echo "✓ All checks passed!" >> "$REPORT"
    echo "" >> "$REPORT"
    echo "✓ Validation successful!"
else
    echo "Issues found: $ERRORS errors, $WARNINGS warnings" >> "$REPORT"
    echo "" >> "$REPORT"
    echo "⚠ Validation completed with issues"
fi

cat "$REPORT"

exit $ERRORS
