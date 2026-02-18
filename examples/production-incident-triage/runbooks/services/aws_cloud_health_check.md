## AWS Cloud Health & Resilience Playbook

This guide assumes you have the **AWS CLI** configured and `jq` installed for parsing.

---

### 1. Identity & Access (The Security Pulse)

A "weak" cloud often has identity bloat or unmonitored root activity.

* **Check for Root Account Usage:** The root account should almost never be used.
```bash
aws iam get-account-summary --query 'SummaryMap.AccountAccessKeysPresent'

```


* **Identify Users without MFA:**
```bash
aws iam list-users --query 'Users[*].UserName' --output text | xargs -I {} aws iam list-mfa-devices --user-name {} --query 'MFADevices[0].SerialNumber' --output text

```


* **Credential Exposure:** Check for access keys older than 90 days (a common audit fail).
```bash
aws iam list-users --query 'Users[].UserName' -o text | xargs -n1 aws iam list-access-keys --user-name

```



---

### 2. Compute Health (EC2 & Restarts)

Sudden restarts or "Scheduled Events" are signs of underlying hardware degradation or maintenance needs.

* **Check for System Reboots/Retires:** This command identifies instances scheduled for maintenance or retirement by AWS.
```bash
aws ec2 describe-instance-status --filter Name=event.code,Values=instance-reboot,system-reboot,instance-retirement

```


* **Monitor Failed Status Checks:** If `StatusCheckFailed_System` is high, the hardware is failing.
```bash
aws ec2 describe-instance-status --query "InstanceStatuses[?InstanceStatus.Status != 'ok' || SystemStatus.Status != 'ok']"

```



---

### 3. Storage & Database (The Integrity Check)

Signs of weakness here include "Stale" snapshots or unencrypted volumes.

* **Identify Unattached EBS Volumes:** These are often "orphaned" during crashes and waste money.
```bash
aws ec2 describe-volumes --filters Name=status,Values=available --query "Volumes[*].{ID:VolumeId,Size:Size}"

```


* **RDS Failover/Restart Events:** Check the last 24 hours of RDS events for unexpected "Restarts" or "Failovers."
```bash
aws rds describe-events --source-type db-instance --duration 1440 --query "Events[?contains(Message, 'restarted') || contains(Message, 'failover')]"

```



---

### 4. Network & Security Audits

Signs of weakness: Open ports to the world or disabled logging.

* **Check for "Open to World" Security Groups:** Looks for any rule allowing `0.0.0.0/0` on sensitive ports (SSH/RDP).
```bash
aws ec2 describe-security-groups --filters Name=ip-permission.cidr,Values='0.0.0.0/0' --query "SecurityGroups[*].{Name:GroupName,ID:GroupId}"

```


* **Verify CloudTrail Status:** If logging is off, you are flying blind.
```bash
aws cloudtrail describe-trails --query "trailList[*].{Name:Name,IsLogging:IncludeGlobalServiceEvents}"

```



---

### 5. Automated Health Alerts (AWS Health Dashboard)

AWS actually tells you when it’s feeling sick. This command pulls from the **AWS Health API** (requires Business or Enterprise support).

* **Query Active Issues:**
```bash
aws health describe-events --filter "eventStatusCodes=open" --region us-east-1

```



---

### Summary Checklist for "Weakness"

| Sign of Weakness | Action |
| --- | --- |
| **High `CPUUtilization**` | Check for "Steal Time" on T-series instances (Credit exhaustion). |
| **Unencrypted Resources** | Scan S3 buckets and EBS volumes for `Encryption: false`. |
| **IAM Policy Over-permissiveness** | Run `aws iam get-account-authorization-details` to find `Resource: *`. |
| **Spiking Latency** | Check ALB `TargetResponseTime` in CloudWatch. |

> **Pro-Tip:** If you see frequent `InternalError` responses from AWS APIs, check your **Service Quotas**. Reaching a limit often looks like a system failure when it's actually a throttle.
