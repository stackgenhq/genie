---
name: aws-security-audit
description: Perform a security audit on AWS IAM users, access keys, MFA status, and security group configurations
---

# AWS Security Audit

Audit AWS account security posture covering IAM users, MFA enforcement, access key age, inactive users, and permissive security groups.

## When to Use This Skill

Use this skill when:
- A security audit or compliance review is needed
- Checking IAM hygiene (MFA, stale credentials, inactive users)
- Auditing network security (open security groups)
- Preparing for SOC 2, ISO 27001, or similar compliance reviews

## Prerequisites

- AWS CLI configured with credentials
- IAM read access (`iam:GenerateCredentialReport`, `iam:GetCredentialReport`, `iam:List*`)
- EC2 read access (`ec2:DescribeSecurityGroups`)

## Workflow

### Step 1: Generate and Fetch IAM Credential Report

```bash
# Generate the credential report
aws iam generate-credential-report

# Wait for report generation (usually < 10 seconds)
sleep 10

# Download and decode the report
aws iam get-credential-report --query Content --output text | base64 -d > /tmp/iam_report.csv

# Display the report
cat /tmp/iam_report.csv
```

### Step 2: Analyze IAM Security

```bash
# Users without MFA enabled (who have console access)
awk -F',' 'NR>1 && $4=="true" && $8=="false" {print "NO MFA: "$1}' /tmp/iam_report.csv

# Access keys older than 1 year
awk -F',' 'NR>1 && $9=="true" {
  cmd = "date -d \"" $10 "\" +%s"; cmd | getline created; close(cmd);
  cmd2 = "date +%s"; cmd2 | getline now; close(cmd2);
  age_days = (now - created) / 86400;
  if (age_days > 365) print "STALE KEY: "$1" (key1, "$age_days" days old)"
}' /tmp/iam_report.csv

# Users who haven't logged in for 1 year
awk -F',' 'NR>1 && $4=="true" && $5!="no_information" && $5!="N/A" {
  cmd = "date -d \"" $5 "\" +%s"; cmd | getline last_login; close(cmd);
  cmd2 = "date +%s"; cmd2 | getline now; close(cmd2);
  age_days = (now - last_login) / 86400;
  if (age_days > 365) print "INACTIVE: "$1" (last login "$age_days" days ago)"
}' /tmp/iam_report.csv

# Overly permissive IAM policies (Resource: *)
aws iam list-policies --scope Local --query "Policies[].{Name:PolicyName,Arn:Arn}" --output text | \
  while read name arn; do
    version=$(aws iam get-policy --policy-arn "$arn" --query "Policy.DefaultVersionId" --output text)
    aws iam get-policy-version --policy-arn "$arn" --version-id "$version" --query "PolicyVersion.Document" --output json | \
      grep -l '"Resource": "\*"' && echo "OVERLY PERMISSIVE: $name"
  done
```

### Step 3: Audit Security Groups

```bash
# Security groups with 0.0.0.0/0 ingress (open to world)
aws ec2 describe-security-groups \
  --filters Name=ip-permission.cidr,Values='0.0.0.0/0' \
  --query "SecurityGroups[].{Name:GroupName,ID:GroupId,Description:Description,OpenPorts:IpPermissions[?contains(IpRanges[].CidrIp,'0.0.0.0/0')].{From:FromPort,To:ToPort,Protocol:IpProtocol}}" \
  --output json
```

### Step 4: Generate Report

Present the output in this format:
1. **MFA Status** — count and list of users without MFA
2. **Stale Access Keys** — users with access keys older than 1 year
3. **Inactive Users** — users who haven't logged in for 1+ year
4. **Overly Permissive Policies** — policies with `Resource: *`
5. **Open Security Groups** — SGs allowing 0.0.0.0/0 (excluding port 443)
6. **Recommendations** — prioritized remediation steps

## Output

Present findings organized by severity:
- **Critical**: Open security groups on non-HTTPS ports, overly permissive IAM policies
- **High**: Users without MFA, stale access keys
- **Medium**: Inactive users, unused security groups
