# Cloud Providers Runbook

Reference guide for AWS, GCP, and Azure resource management, cost analysis, and security.

---

## AWS

### Resource Discovery

```bash
# EC2 instances — overview
aws ec2 describe-instances --region $REGION \
  --query "Reservations[].Instances[].{ID:InstanceId,Type:InstanceType,State:State.Name,Name:Tags[?Key=='Name']|[0].Value}" \
  --output table

# S3 buckets
aws s3api list-buckets --query "Buckets[].{Name:Name,Created:CreationDate}" --output table

# RDS databases
aws rds describe-db-instances \
  --query "DBInstances[].{ID:DBInstanceIdentifier,Engine:Engine,Status:DBInstanceStatus,Class:DBInstanceClass}" \
  --output table

# EKS clusters
aws eks list-clusters --region $REGION
aws eks describe-cluster --name $CLUSTER --region $REGION --query 'cluster.{status:status,version:version}'
```

### Cost Analysis

```bash
# Monthly cost breakdown by service
aws ce get-cost-and-usage \
  --time-period Start=$(date -u -d 'first day of last month' +%Y-%m-%d),End=$(date -u -d 'first day of this month' +%Y-%m-%d) \
  --granularity MONTHLY \
  --metrics BlendedCost \
  --group-by Type=DIMENSION,Key=SERVICE

# Daily cost trend (last 14 days)
aws ce get-cost-and-usage \
  --time-period Start=$(date -u -d '14 days ago' +%Y-%m-%d),End=$(date -u +%Y-%m-%d) \
  --granularity DAILY \
  --metrics BlendedCost

# Cost comparison month-over-month
aws ce get-cost-and-usage \
  --time-period Start=$(date -u -d '2 months ago' +%Y-%m-01),End=$(date -u +%Y-%m-01) \
  --granularity MONTHLY \
  --metrics BlendedCost \
  --group-by Type=DIMENSION,Key=SERVICE
```

### Security Audit

```bash
# IAM users without MFA
aws iam generate-credential-report && sleep 5 && \
aws iam get-credential-report --query Content --output text | base64 -d | \
  awk -F',' 'NR>1 && $4=="true" && $8=="false" {print $1}'

# Access keys older than 90 days
aws iam list-users --query "Users[].UserName" --output text | \
  xargs -I{} aws iam list-access-keys --user-name {} \
  --query "AccessKeyMetadata[?CreateDate<='$(date -u -d '90 days ago' +%Y-%m-%dT%H:%M:%SZ)']"

# Open security groups (0.0.0.0/0)
aws ec2 describe-security-groups \
  --filters Name=ip-permission.cidr,Values='0.0.0.0/0' \
  --query "SecurityGroups[].{Name:GroupName,ID:GroupId,Ports:IpPermissions[].{From:FromPort,To:ToPort}}"

# Public S3 buckets
for bucket in $(aws s3api list-buckets --query "Buckets[].Name" --output text); do
  policy=$(aws s3api get-bucket-policy-status --bucket $bucket 2>/dev/null)
  echo "$bucket: $policy"
done

# CloudTrail recent events (last 2 hours)
aws cloudtrail lookup-events --start-time $(date -u -d '2 hours ago' +%s) \
  --query "Events[].{Time:EventTime,User:Username,Action:EventName}" --output table
```

### Instance Health

```bash
# EC2 status checks
aws ec2 describe-instance-status --region $REGION \
  --query "InstanceStatuses[?InstanceStatus.Status!='ok' || SystemStatus.Status!='ok']"

# Scheduled maintenance events
aws ec2 describe-instance-status --region $REGION \
  --filters Name=instance-state-name,Values=running \
  --query "InstanceStatuses[?Events!=null].{ID:InstanceId,Events:Events}"
```

---

## GCP

### Resource Discovery

```bash
# Compute instances
gcloud compute instances list --format="table(name,zone,machineType.basename(),status,networkInterfaces[0].accessConfigs[0].natIP)"

# GKE clusters
gcloud container clusters list --format="table(name,zone,status,currentMasterVersion,currentNodeCount)"

# Cloud SQL instances
gcloud sql instances list --format="table(name,databaseVersion,state,tier,region)"

# Cloud Storage buckets
gcloud storage buckets list --format="table(name,location,storageClass)"
```

### Cost Analysis

```bash
# Export billing data (requires BigQuery billing export)
bq query --use_legacy_sql=false "
  SELECT service.description, SUM(cost) as total_cost
  FROM \`project.billing_dataset.gcp_billing_export\`
  WHERE DATE(usage_start_time) >= DATE_SUB(CURRENT_DATE(), INTERVAL 30 DAY)
  GROUP BY 1 ORDER BY 2 DESC"
```

---

## Azure

### Resource Discovery

```bash
# Virtual machines
az vm list --output table --query "[].{Name:name,RG:resourceGroup,Size:hardwareProfile.vmSize,State:powerState}"

# AKS clusters
az aks list --output table --query "[].{Name:name,RG:resourceGroup,K8sVersion:kubernetesVersion,Nodes:agentPoolProfiles[0].count}"

# SQL databases
az sql db list --server $SERVER --resource-group $RG --output table

# Storage accounts
az storage account list --output table --query "[].{Name:name,Kind:kind,Tier:sku.tier,Location:location}"
```

### Cost Analysis

```bash
# Current month cost
az consumption usage list --start-date $(date -u -d 'first day of this month' +%Y-%m-%d) \
  --end-date $(date -u +%Y-%m-%d) \
  --query "[].{Service:meterCategory,Cost:pretaxCost}" --output table
```

---

## Cross-Cloud Best Practices

- **Always specify region** explicitly to avoid silent failures
- **Use read-only operations first** (`describe`, `list`, `get`)
- **Tag filtering** — use tags to scope queries: `--filters Name=tag:Environment,Values=production`
- **Output as JSON** when data needs further processing: `--output json | jq '...'`
- **IAM principle of least privilege** — audit overly broad policies regularly
