---
name: aws-cost-analyzer
description: Analyze AWS resources for cost optimization opportunities and generate savings recommendations
---

# AWS Cost Analyzer

Analyzes your AWS infrastructure to identify cost optimization opportunities including underutilized resources, inefficient configurations, and potential savings through reserved instances or spot instances.

## When to Use This Skill

Use this skill when you need to:
- Identify cost optimization opportunities
- Find underutilized or idle resources
- Analyze EC2 instance right-sizing
- Evaluate reserved instance opportunities
- Review storage costs and optimization
- Generate cost reduction recommendations

## Usage

### Full Cost Analysis

```bash
python3 scripts/analyze_costs.py --region <aws-region> [--profile <aws-profile>]
```

### Specific Resource Analysis

```bash
# Analyze EC2 instances only
python3 scripts/analyze_costs.py --region us-east-1 --resource-type ec2

# Analyze RDS databases
python3 scripts/analyze_costs.py --region us-east-1 --resource-type rds

# Analyze S3 storage
python3 scripts/analyze_costs.py --region us-east-1 --resource-type s3
```

## Analysis Categories

### 1. EC2 Instances
- Underutilized instances (low CPU/memory usage)
- Stopped instances still incurring costs
- Instance right-sizing recommendations
- Reserved instance opportunities
- Spot instance candidates

### 2. EBS Volumes
- Unattached volumes
- Over-provisioned IOPS
- gp2 to gp3 migration opportunities
- Snapshot cleanup recommendations

### 3. RDS Databases
- Underutilized database instances
- Multi-AZ when not needed
- Storage optimization
- Reserved instance opportunities

### 4. S3 Storage
- Lifecycle policy recommendations
- Intelligent-Tiering opportunities
- Glacier migration candidates
- Incomplete multipart uploads

### 5. Load Balancers
- Idle load balancers (no targets)
- Consolidation opportunities
- ALB vs NLB optimization

### 6. Elastic IPs
- Unattached Elastic IPs
- Unused NAT Gateways

## Output

Generates comprehensive reports in `$OUTPUT_DIR`:
- `cost_analysis.json`: Detailed findings in JSON format
- `recommendations.md`: Prioritized recommendations
- `savings_summary.txt`: Estimated monthly savings
- `resource_inventory.csv`: Full resource inventory

## Example Output

```
AWS Cost Analysis Report
========================

Region: us-east-1
Analysis Date: 2024-02-08
Total Resources Analyzed: 127

ESTIMATED MONTHLY SAVINGS: $2,847

HIGH PRIORITY RECOMMENDATIONS (Potential savings: $1,920/month):

1. EC2 Instance Right-Sizing (5 instances)
   Current Cost: $1,200/month
   Optimized Cost: $600/month
   Savings: $600/month (50%)
   
   Details:
   - i-abc123: t3.2xlarge → t3.xlarge (avg CPU: 15%)
   - i-def456: m5.4xlarge → m5.2xlarge (avg CPU: 22%)
   - i-ghi789: c5.9xlarge → c5.4xlarge (avg CPU: 18%)

2. Unattached EBS Volumes (12 volumes)
   Monthly Cost: $240
   Recommendation: Delete or attach
   Savings: $240/month
   
3. Stopped EC2 Instances (8 instances)
   EBS Cost: $320/month
   Recommendation: Terminate or create AMI
   Savings: $320/month

MEDIUM PRIORITY (Potential savings: $720/month):

4. gp2 to gp3 Migration (45 volumes)
   Current Cost: $900/month
   Optimized Cost: $720/month
   Savings: $180/month (20%)

5. S3 Lifecycle Policies (3 buckets)
   Current Cost: $540/month
   With Intelligent-Tiering: $360/month
   Savings: $180/month (33%)

6. Reserved Instance Opportunities
   On-Demand Cost: $1,800/month
   1-Year RI Cost: $1,440/month
   Savings: $360/month (20%)

LOW PRIORITY (Potential savings: $207/month):

7. Idle Load Balancers (3 ALBs)
   Monthly Cost: $75
   Recommendation: Delete unused
   
8. Unattached Elastic IPs (4 IPs)
   Monthly Cost: $12
   Recommendation: Release
```

## AWS Permissions Required

The script requires read-only access to AWS resources:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:Describe*",
        "rds:Describe*",
        "s3:ListAllMyBuckets",
        "s3:GetBucketLocation",
        "s3:GetBucketTagging",
        "elasticloadbalancing:Describe*",
        "cloudwatch:GetMetricStatistics"
      ],
      "Resource": "*"
    }
  ]
}
```

## Dependencies

- Python 3.8+
- boto3 (AWS SDK for Python)
- AWS credentials configured
- CloudWatch metrics access (for utilization data)

## Configuration

Set AWS credentials:
```bash
export AWS_ACCESS_KEY_ID=your_key
export AWS_SECRET_ACCESS_KEY=your_secret
# Or use AWS CLI profile
export AWS_PROFILE=your_profile
```

## Best Practices

### Regular Analysis
- Run weekly for production environments
- Run monthly for development environments
- Track savings over time
- Set up automated reports

### Acting on Recommendations
1. Start with high-priority items
2. Test changes in non-production first
3. Monitor impact after changes
4. Document decisions and exceptions

### Cost Governance
- Set up budget alerts
- Tag resources for cost allocation
- Review recommendations with teams
- Implement approved changes promptly
