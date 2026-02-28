---
name: terraform-validate
description: Validate Terraform configurations and enforce best practices for infrastructure as code
---

# Terraform Validate

Comprehensive validation of Terraform configurations including syntax checking, security best practices, cost optimization, and compliance verification.

## When to Use This Skill

Use this skill when you need to:
- Validate Terraform syntax and configuration
- Enforce security best practices
- Check for cost optimization opportunities
- Ensure compliance with organizational standards
- Prepare infrastructure code for review
- Prevent common Terraform mistakes

## Usage

### Run Full Validation

```bash
bash scripts/validate.sh <terraform-directory>
```

This runs:
1. `terraform fmt -check` - Format validation
2. `terraform validate` - Syntax validation
3. Security checks - Best practices validation
4. Cost estimation - Resource cost analysis
5. Compliance checks - Policy validation

### Format Check Only

```bash
bash scripts/validate.sh <terraform-directory> --format-only
```

### Security Scan Only

```bash
bash scripts/validate.sh <terraform-directory> --security-only
```

## Validation Checks

### 1. Syntax and Format
- Terraform HCL syntax correctness
- Consistent formatting (terraform fmt)
- Valid resource references
- Proper variable usage
- Module source validation

### 2. Security Best Practices
- No hardcoded credentials
- Encryption enabled for storage
- Security groups not overly permissive (0.0.0.0/0)
- IAM policies follow least privilege
- S3 buckets not publicly accessible
- Secrets use variables or data sources

### 3. Cost Optimization
- Instance types appropriate for workload
- Reserved instances considered
- Auto-scaling configured
- Unused resources identified
- Storage tiers optimized

### 4. Compliance
- Required tags present
- Naming conventions followed
- Backup policies configured
- Logging enabled
- Monitoring configured

### 5. Best Practices
- State backend configured
- Workspaces used appropriately
- Modules versioned
- Outputs documented
- Variables have descriptions

## Output

Generates comprehensive report in `$OUTPUT_DIR`:
- `validation_report.txt`: Full validation results
- `security_issues.json`: Security findings
- `cost_estimate.txt`: Estimated monthly costs
- `recommendations.md`: Actionable recommendations

## Example Output

```
Terraform Validation Report
============================

Directory: ./infrastructure
Files: 12 .tf files
Modules: 3

✓ PASSED: Terraform format check
✓ PASSED: Terraform validate
✗ FAILED: Security checks (3 issues)
⚠ WARNING: Cost optimization (2 recommendations)

SECURITY ISSUES (3):
  CRITICAL: aws_security_group.web allows 0.0.0.0/0 on port 22
    File: security_groups.tf:15
    Fix: Restrict SSH access to specific IP ranges

  HIGH: aws_s3_bucket.data missing encryption configuration
    File: storage.tf:8
    Fix: Add server_side_encryption_configuration block

  MEDIUM: Hardcoded AWS region in provider
    File: main.tf:3
    Fix: Use variable for region

COST OPTIMIZATION (2):
  ⚠ EC2 instance t3.2xlarge may be oversized
    Recommendation: Consider t3.xlarge (50% cost reduction)

  ⚠ EBS volumes using gp2 instead of gp3
    Recommendation: Migrate to gp3 (20% cost reduction)

RECOMMENDATIONS:
  1. Enable encryption for all S3 buckets
  2. Restrict security group rules
  3. Add required tags: Environment, Owner, CostCenter
  4. Configure remote state backend
  5. Add lifecycle policies for S3 buckets
```

## Dependencies

- Terraform 1.0+
- bash 4.0+
- Optional: tfsec (for enhanced security scanning)
- Optional: terraform-cost-estimation (for cost analysis)

## Integration with CI/CD

Add to your CI pipeline:

```yaml
# .github/workflows/terraform.yml
- name: Validate Terraform
  run: |
    export OUTPUT_DIR=./terraform-validation
    bash scripts/validate.sh ./infrastructure
    
- name: Upload Report
  uses: actions/upload-artifact@v2
  with:
    name: terraform-validation-report
    path: terraform-validation/
```

## Best Practices

### Before Committing
1. Run validation locally
2. Fix all critical and high issues
3. Review cost estimates
4. Update documentation

### In Code Reviews
1. Share validation report
2. Explain any warnings
3. Document exceptions
4. Verify compliance

### For Teams
1. Enforce validation in CI/CD
2. Set quality gates
3. Track metrics over time
4. Share common issues
