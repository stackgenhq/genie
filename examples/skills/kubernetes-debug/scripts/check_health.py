#!/usr/bin/env python3
"""
Check health status of a Kubernetes pod.
"""
import os
import subprocess
import json
import argparse

def check_pod_health(namespace, pod_name):
    """Analyze pod health including probes and resource usage."""
    # Get pod details in JSON format
    cmd = ["kubectl", "get", "pod", pod_name, "-n", namespace, "-o", "json"]
    
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        pod_data = json.loads(result.stdout)
        
        health_report = {
            "pod_name": pod_name,
            "namespace": namespace,
            "phase": pod_data.get("status", {}).get("phase"),
            "conditions": pod_data.get("status", {}).get("conditions", []),
            "container_statuses": [],
            "resource_usage": {}
        }
        
        # Analyze container statuses
        for container in pod_data.get("status", {}).get("containerStatuses", []):
            health_report["container_statuses"].append({
                "name": container.get("name"),
                "ready": container.get("ready"),
                "restart_count": container.get("restartCount"),
                "state": container.get("state")
            })
        
        # Get resource requests and limits
        for container in pod_data.get("spec", {}).get("containers", []):
            resources = container.get("resources", {})
            health_report["resource_usage"][container.get("name")] = {
                "requests": resources.get("requests", {}),
                "limits": resources.get("limits", {})
            }
        
        return health_report
        
    except subprocess.CalledProcessError as e:
        return {"error": f"Failed to get pod health: {e.stderr}"}
    except json.JSONDecodeError as e:
        return {"error": f"Failed to parse pod data: {str(e)}"}

def main():
    parser = argparse.ArgumentParser(description="Check Kubernetes pod health")
    parser.add_argument("namespace", help="Kubernetes namespace")
    parser.add_argument("pod_name", help="Pod name")
    
    args = parser.parse_args()
    
    # Check pod health
    health_report = check_pod_health(args.namespace, args.pod_name)
    
    # Write to output directory
    output_dir = os.environ.get("OUTPUT_DIR", "./output")
    os.makedirs(output_dir, exist_ok=True)
    
    output_file = os.path.join(output_dir, "health_report.json")
    with open(output_file, "w") as f:
        json.dump(health_report, f, indent=2)
    
    print(f"Health report written to {output_file}")
    
    # Print summary
    if "error" not in health_report:
        print(f"Pod Phase: {health_report['phase']}")
        print(f"Containers: {len(health_report['container_statuses'])}")
        for container in health_report["container_statuses"]:
            status = "✓" if container["ready"] else "✗"
            print(f"  {status} {container['name']} (restarts: {container['restart_count']})")

if __name__ == "__main__":
    main()
