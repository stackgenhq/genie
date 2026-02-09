#!/usr/bin/env python3
"""
Describe a Kubernetes resource in detail.
"""
import os
import subprocess
import argparse

def describe_resource(namespace, resource_type, resource_name):
    """Get detailed description of a Kubernetes resource."""
    cmd = ["kubectl", "describe", resource_type, resource_name, "-n", namespace]
    
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        return result.stdout
    except subprocess.CalledProcessError as e:
        return f"Error describing resource: {e.stderr}"

def main():
    parser = argparse.ArgumentParser(description="Describe Kubernetes resource")
    parser.add_argument("namespace", help="Kubernetes namespace")
    parser.add_argument("resource_type", help="Resource type (pod, service, deployment, etc.)")
    parser.add_argument("resource_name", help="Resource name")
    
    args = parser.parse_args()
    
    # Get resource description
    description = describe_resource(args.namespace, args.resource_type, args.resource_name)
    
    # Write to output directory
    output_dir = os.environ.get("OUTPUT_DIR", "./output")
    os.makedirs(output_dir, exist_ok=True)
    
    output_file = os.path.join(output_dir, "describe.yaml")
    with open(output_file, "w") as f:
        f.write(description)
    
    print(f"Description written to {output_file}")
    print(f"Resource: {args.resource_type}/{args.resource_name} in namespace {args.namespace}")

if __name__ == "__main__":
    main()
