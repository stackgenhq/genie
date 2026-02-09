#!/usr/bin/env python3
"""
Get logs from a Kubernetes pod with filtering options.
"""
import os
import subprocess
import argparse

def get_pod_logs(namespace, pod_name, tail=100, previous=False, container=None):
    """Retrieve logs from a Kubernetes pod."""
    cmd = ["kubectl", "logs", "-n", namespace, pod_name]
    
    if tail:
        cmd.extend(["--tail", str(tail)])
    
    if previous:
        cmd.append("--previous")
    
    if container:
        cmd.extend(["-c", container])
    
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        return result.stdout
    except subprocess.CalledProcessError as e:
        return f"Error getting logs: {e.stderr}"

def main():
    parser = argparse.ArgumentParser(description="Get Kubernetes pod logs")
    parser.add_argument("namespace", help="Kubernetes namespace")
    parser.add_argument("pod_name", help="Pod name")
    parser.add_argument("--tail", type=int, default=100, help="Number of lines to show")
    parser.add_argument("--previous", action="store_true", help="Show previous container logs")
    parser.add_argument("--container", help="Container name (for multi-container pods)")
    
    args = parser.parse_args()
    
    # Get logs
    logs = get_pod_logs(
        args.namespace,
        args.pod_name,
        tail=args.tail,
        previous=args.previous,
        container=args.container
    )
    
    # Write to output directory
    output_dir = os.environ.get("OUTPUT_DIR", "./output")
    os.makedirs(output_dir, exist_ok=True)
    
    output_file = os.path.join(output_dir, "logs.txt")
    with open(output_file, "w") as f:
        f.write(logs)
    
    print(f"Logs written to {output_file}")
    print(f"Retrieved {len(logs.splitlines())} lines")

if __name__ == "__main__":
    main()
