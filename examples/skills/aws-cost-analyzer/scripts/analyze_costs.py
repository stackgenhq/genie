#!/usr/bin/env python3
"""
Analyze AWS resources for cost optimization opportunities.
"""
import os
import sys
import json
import argparse
from datetime import datetime, timedelta

try:
    import boto3
    from botocore.exceptions import ClientError, NoCredentialsError
except ImportError:
    print("Error: boto3 is required. Install with: pip install boto3")
    sys.exit(1)

def analyze_ec2_instances(ec2_client, cloudwatch_client):
    """Analyze EC2 instances for optimization opportunities."""
    recommendations = []
    
    try:
        response = ec2_client.describe_instances()
        
        for reservation in response['Reservations']:
            for instance in reservation['Instances']:
                instance_id = instance['InstanceId']
                instance_type = instance['InstanceType']
                state = instance['State']['Name']
                
                # Check for stopped instances
                if state == 'stopped':
                    recommendations.append({
                        'priority': 'HIGH',
                        'resource_type': 'EC2',
                        'resource_id': instance_id,
                        'issue': 'Stopped instance incurring EBS costs',
                        'recommendation': 'Terminate or create AMI if no longer needed',
                        'estimated_monthly_savings': 40  # Approximate EBS cost
                    })
                
                # Check for underutilized running instances
                elif state == 'running':
                    # Get CPU utilization from CloudWatch
                    try:
                        cpu_stats = cloudwatch_client.get_metric_statistics(
                            Namespace='AWS/EC2',
                            MetricName='CPUUtilization',
                            Dimensions=[{'Name': 'InstanceId', 'Value': instance_id}],
                            StartTime=datetime.utcnow() - timedelta(days=7),
                            EndTime=datetime.utcnow(),
                            Period=3600,
                            Statistics=['Average']
                        )
                        
                        if cpu_stats['Datapoints']:
                            avg_cpu = sum(dp['Average'] for dp in cpu_stats['Datapoints']) / len(cpu_stats['Datapoints'])
                            
                            if avg_cpu < 20:
                                recommendations.append({
                                    'priority': 'HIGH',
                                    'resource_type': 'EC2',
                                    'resource_id': instance_id,
                                    'instance_type': instance_type,
                                    'issue': f'Low CPU utilization ({avg_cpu:.1f}%)',
                                    'recommendation': 'Consider downsizing instance type',
                                    'estimated_monthly_savings': 100  # Approximate
                                })
                    except Exception as e:
                        print(f"Warning: Could not get metrics for {instance_id}: {e}")
        
    except ClientError as e:
        print(f"Error analyzing EC2 instances: {e}")
    
    return recommendations

def analyze_ebs_volumes(ec2_client):
    """Analyze EBS volumes for optimization opportunities."""
    recommendations = []
    
    try:
        response = ec2_client.describe_volumes()
        
        for volume in response['Volumes']:
            volume_id = volume['VolumeId']
            volume_type = volume['VolumeType']
            size = volume['Size']
            state = volume['State']
            
            # Check for unattached volumes
            if state == 'available':
                cost = size * 0.10  # Approximate cost per GB
                recommendations.append({
                    'priority': 'HIGH',
                    'resource_type': 'EBS',
                    'resource_id': volume_id,
                    'issue': 'Unattached volume',
                    'recommendation': 'Delete if no longer needed or attach to instance',
                    'estimated_monthly_savings': cost
                })
            
            # Check for gp2 to gp3 migration opportunity
            if volume_type == 'gp2' and size > 100:
                savings = size * 0.02  # Approximate 20% savings
                recommendations.append({
                    'priority': 'MEDIUM',
                    'resource_type': 'EBS',
                    'resource_id': volume_id,
                    'issue': 'Using gp2 instead of gp3',
                    'recommendation': 'Migrate to gp3 for better price/performance',
                    'estimated_monthly_savings': savings
                })
    
    except ClientError as e:
        print(f"Error analyzing EBS volumes: {e}")
    
    return recommendations

def analyze_elastic_ips(ec2_client):
    """Analyze Elastic IPs for unused addresses."""
    recommendations = []
    
    try:
        response = ec2_client.describe_addresses()
        
        for address in response['Addresses']:
            allocation_id = address.get('AllocationId', 'N/A')
            public_ip = address.get('PublicIp', 'N/A')
            
            # Check if EIP is not associated with an instance
            if 'InstanceId' not in address:
                recommendations.append({
                    'priority': 'LOW',
                    'resource_type': 'EIP',
                    'resource_id': allocation_id,
                    'public_ip': public_ip,
                    'issue': 'Unattached Elastic IP',
                    'recommendation': 'Release if not needed',
                    'estimated_monthly_savings': 3.60  # $0.005/hour
                })
    
    except ClientError as e:
        print(f"Error analyzing Elastic IPs: {e}")
    
    return recommendations

def generate_report(recommendations, output_dir):
    """Generate cost analysis report."""
    os.makedirs(output_dir, exist_ok=True)
    
    # Calculate total savings
    total_savings = sum(r.get('estimated_monthly_savings', 0) for r in recommendations)
    
    # Group by priority
    high_priority = [r for r in recommendations if r['priority'] == 'HIGH']
    medium_priority = [r for r in recommendations if r['priority'] == 'MEDIUM']
    low_priority = [r for r in recommendations if r['priority'] == 'LOW']
    
    # Generate JSON report
    json_report = {
        'analysis_date': datetime.now().isoformat(),
        'total_recommendations': len(recommendations),
        'estimated_monthly_savings': total_savings,
        'recommendations': recommendations
    }
    
    with open(os.path.join(output_dir, 'cost_analysis.json'), 'w') as f:
        json.dump(json_report, f, indent=2)
    
    # Generate text summary
    summary = f"""AWS Cost Analysis Report
========================

Analysis Date: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}
Total Recommendations: {len(recommendations)}
Estimated Monthly Savings: ${total_savings:.2f}

HIGH PRIORITY ({len(high_priority)} items, ${sum(r.get('estimated_monthly_savings', 0) for r in high_priority):.2f}/month):
"""
    
    for i, rec in enumerate(high_priority, 1):
        summary += f"\n{i}. {rec['resource_type']} - {rec['resource_id']}"
        summary += f"\n   Issue: {rec['issue']}"
        summary += f"\n   Recommendation: {rec['recommendation']}"
        summary += f"\n   Savings: ${rec.get('estimated_monthly_savings', 0):.2f}/month\n"
    
    summary += f"\nMEDIUM PRIORITY ({len(medium_priority)} items, ${sum(r.get('estimated_monthly_savings', 0) for r in medium_priority):.2f}/month):\n"
    for i, rec in enumerate(medium_priority, 1):
        summary += f"\n{i}. {rec['resource_type']} - {rec['resource_id']}"
        summary += f"\n   {rec['recommendation']}\n"
    
    summary += f"\nLOW PRIORITY ({len(low_priority)} items, ${sum(r.get('estimated_monthly_savings', 0) for r in low_priority):.2f}/month):\n"
    for i, rec in enumerate(low_priority, 1):
        summary += f"\n{i}. {rec['resource_type']} - {rec['resource_id']}"
        summary += f"\n   {rec['recommendation']}\n"
    
    with open(os.path.join(output_dir, 'savings_summary.txt'), 'w') as f:
        f.write(summary)
    
    return summary

def main():
    parser = argparse.ArgumentParser(description="Analyze AWS costs and find optimization opportunities")
    parser.add_argument('--region', default='us-east-1', help='AWS region')
    parser.add_argument('--profile', help='AWS profile name')
    parser.add_argument('--resource-type', choices=['ec2', 'ebs', 'eip', 'all'], default='all',
                       help='Resource type to analyze')
    
    args = parser.parse_args()
    
    try:
        # Initialize AWS clients
        session = boto3.Session(profile_name=args.profile, region_name=args.region)
        ec2_client = session.client('ec2')
        cloudwatch_client = session.client('cloudwatch')
        
        print(f"Analyzing AWS resources in {args.region}...")
        
        recommendations = []
        
        # Run analyses based on resource type
        if args.resource_type in ['ec2', 'all']:
            print("Analyzing EC2 instances...")
            recommendations.extend(analyze_ec2_instances(ec2_client, cloudwatch_client))
        
        if args.resource_type in ['ebs', 'all']:
            print("Analyzing EBS volumes...")
            recommendations.extend(analyze_ebs_volumes(ec2_client))
        
        if args.resource_type in ['eip', 'all']:
            print("Analyzing Elastic IPs...")
            recommendations.extend(analyze_elastic_ips(ec2_client))
        
        # Generate report
        output_dir = os.environ.get('OUTPUT_DIR', './output')
        summary = generate_report(recommendations, output_dir)
        
        print(f"\n{summary}")
        print(f"\nDetailed report saved to {output_dir}/cost_analysis.json")
        
    except NoCredentialsError:
        print("Error: AWS credentials not found. Configure credentials or use --profile")
        sys.exit(1)
    except ClientError as e:
        print(f"Error: {e}")
        sys.exit(1)

if __name__ == '__main__':
    main()
