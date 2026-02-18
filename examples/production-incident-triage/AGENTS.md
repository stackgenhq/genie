# Production Incident Triage — Kubernetes & AWS Infrastructure

> **Audience:** DevOps/SREs and Developers
> **Scenario:** High error rates, latency spikes, or "flaky" infrastructure.

## Updated Agent Behavior Rules

### Tool Selection & Command Batching

* **Batch AWS and K8s checks:** When checking a service, combine `kubectl` and `aws` CLI calls into one `run_shell` to see the full stack at once.
* **Region Awareness:** Always specify `--region` in AWS commands if not set in the environment to avoid silent failures.

---

## 1. The "Vital Signs" Check (Direct — No Sub-Agent)

Run this to see if the problem is the Code (K8s) or the Cloud (AWS):

```bash
NS="<namespace>"
CLUSTER="<cluster-name>"
REGION="<region>"

echo "=== K8S STATUS ===" && \
kubectl get pods -n $NS -o wide && \
kubectl get events -n $NS --sort-by='.lastTimestamp' | tail -10 && \
echo "=== AWS EKS HEALTH ===" && \
aws eks describe-cluster --name $CLUSTER --region $REGION --query 'cluster.{status:status,health:health}' && \
echo "=== ACTIVE AWS INCIDENTS ===" && \
aws health describe-events --filter "eventStatusCodes=open" --region $REGION --max-items 5

```

---

## 2. Infrastructure Health & "Signs of Weakness"

Use these commands to detect underlying AWS instability that impacts K8s.

### Compute & Node Failures

If nodes are `NotReady` or pods are crashing, check for hardware retirement or credit exhaustion.

```bash
# Check for scheduled maintenance or hardware failure on EKS nodes
aws ec2 describe-instance-status --region $REGION --filters Name=instance-state-name,Values=running --query "InstanceStatuses[?Events!=null]"

# Check for T-series CPU Credit exhaustion (common cause of "slow" pods)
aws cloudwatch get-metric-statistics --namespace AWS/EC2 --metric-name CPUSurplusCreditBalance --dimensions Name=InstanceId,Value=<instance-id> --start-time `date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ` --end-time `date -u +%Y-%m-%dT%H:%M:%SZ` --period 3600 --statistics Average

```

### Networking & Load Balancing

If the app is "healthy" but traffic isn't reaching it.

```bash
# Check ALB/NLB Target Health (Is the Load Balancer actually talking to the Pods?)
TG_ARN=$(aws elbv2 describe-target-groups --query "TargetGroups[?contains(TargetGroupName, '$NS')].TargetGroupArn" --output text)
aws elbv2 describe-target-health --target-group-arn $TG_ARN

# Check for VPC CNI (aws-node) errors (ENI Exhaustion)
kubectl -n kube-system logs -l k8s-app=aws-node --tail=50 | grep -i "error"

```

### Storage & Database Latency

```bash
# Check for RDS Storage Full or High Latency
aws rds describe-db-instances --query "DBInstances[*].{ID:DBInstanceIdentifier,Status:DBInstanceStatus,Storage:AllocatedStorage,StorageType:StorageType}"

# Check for EBS Volume Attachment issues (Stuck 'Pending' pods)
aws ec2 describe-volumes --filters Name=status,Values=attaching,detaching

```

---

## 3. Triage Workflow: AWS vs K8s

| If you see... | AWS Sign of Weakness | K8s Sign of Weakness |
| --- | --- | --- |
| **High Latency** | ALB `TargetResponseTime` spike or EBS `VolumeReadOps` throttled. | Pod `CPU Throttling` (check limits). |
| **Connection Reset** | Security Group/NACL misconfiguration. | CoreDNS crashing or Service mesh (Istio/Linkerd) failure. |
| **Pods stuck Pending** | EC2 Insufficient Capacity or ENI limits reached. | No nodes match `nodeSelector` or Resource Quotas full. |
| **5xx Errors** | ALB `HTTPCode_Target_5XX_Count`. | Application `Panic` or `OOMKilled`. |

---

## 4. Security & Audit Trail

Check if a recent configuration change caused the "weakness."

```bash
# Who changed what in the last 2 hours?
aws cloudtrail lookup-events --start-time $(date -u -d '2 hours ago' +%s) --query "Events[*].{Time:EventTime,User:Username,Action:EventName}"

# Check for sensitive "Open to World" Security Group changes
aws ec2 describe-security-groups --filters Name=ip-permission.cidr,Values='0.0.0.0/0' --query "SecurityGroups[*].{Name:GroupName,ID:GroupId}"

```

## Emergency Command: Full AWS/EKS Health Dump

Use this as a last resort to gather all context for an escalation.

```bash
echo "Cluster Status:" && aws eks describe-cluster --name $CLUSTER --query cluster.status && \
echo "Node Health:" && kubectl get nodes && \
echo "VPC Subnet IPs remaining:" && aws ec2 describe-subnets --query "Subnets[*].{ID:SubnetId,AvailableIPs:AvailableIpAddressCount}" && \
echo "Recent EKS Errors:" && aws logs filter-log-events --log-group-name /aws/eks/$CLUSTER/cluster --filter-pattern "ERROR" --limit 10

```
