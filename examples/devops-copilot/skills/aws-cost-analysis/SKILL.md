---
name: aws-cost-analysis
description: Perform monthly AWS cost analysis with resource breakdown, month-over-month comparison, and root cause identification for cost changes
---

# AWS Cost Analysis

Analyze AWS costs on a monthly basis, compare spending month-over-month, identify which resources drove cost changes, and provide actionable recommendations.

## When to Use This Skill

Use this skill when:
- The user asks about AWS costs, spending trends, or billing
- Monthly or quarterly cost reviews are needed
- Cost anomalies or unexpected charges need investigation
- Resource-level cost optimization is required

## Prerequisites

- AWS CLI configured with credentials (`aws configure` or environment variables)
- Cost Explorer API access (`ce:GetCostAndUsage`)
- CloudWatch read access for utilization data

## Workflow

### Step 1: Fetch Monthly Costs

```bash
# Get cost breakdown by service for the requested months
CURRENT_MONTH=$(date -u +%Y-%m-01)
LAST_MONTH=$(date -u -d 'first day of last month' +%Y-%m-01)
TWO_MONTHS_AGO=$(date -u -d 'first day of 2 months ago' +%Y-%m-01)

aws ce get-cost-and-usage \
  --time-period Start=$TWO_MONTHS_AGO,End=$CURRENT_MONTH \
  --granularity MONTHLY \
  --metrics BlendedCost UnblendedCost UsageQuantity \
  --group-by Type=DIMENSION,Key=SERVICE \
  --output json
```

### Step 2: Identify Cost Drivers

```bash
# Get resource-level cost breakdown for services with >15% change
# For each service with significant change, drill into resources:
aws ce get-cost-and-usage \
  --time-period Start=$LAST_MONTH,End=$CURRENT_MONTH \
  --granularity MONTHLY \
  --metrics BlendedCost \
  --group-by Type=DIMENSION,Key=SERVICE Type=DIMENSION,Key=USAGE_TYPE

# Check for new or terminated resources
aws ce get-cost-and-usage \
  --time-period Start=$TWO_MONTHS_AGO,End=$CURRENT_MONTH \
  --granularity MONTHLY \
  --metrics BlendedCost \
  --group-by Type=DIMENSION,Key=LINKED_ACCOUNT
```

### Step 3: Root Cause Analysis

For each resource where cost change is >15%, investigate:

```bash
# EC2: Check instance types, running hours, spot vs on-demand
aws ec2 describe-instances --query "Reservations[].Instances[].{ID:InstanceId,Type:InstanceType,State:State.Name,Launch:LaunchTime}" --output table

# RDS: Check instance sizes, multi-AZ, storage
aws rds describe-db-instances --query "DBInstances[].{ID:DBInstanceIdentifier,Class:DBInstanceClass,MultiAZ:MultiAZ,Storage:AllocatedStorage}" --output table

# S3: Check storage classes, request counts
aws s3api list-buckets --query "Buckets[].Name" --output text | xargs -I{} aws s3api head-bucket --bucket {} 2>/dev/null
```

### Step 4: Generate Report

Present the output in this format:
1. **Overall Cost** — total for each month
2. **Service Comparison** — table with month-over-month cost per service
3. **Cost Drivers** — reasons for increase/decrease where change >15%
4. **Recommendations** — actionable cost optimization suggestions

## Output

The analysis should produce:
- A summary table of monthly costs by service
- Highlighted services with >15% cost change
- Root cause explanation for significant changes
- Prioritized recommendations with estimated savings
