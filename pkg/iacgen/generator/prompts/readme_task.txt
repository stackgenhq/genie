**Task:** Generate a comprehensive README.md for Infrastructure as Code

**Role:** You are a senior DevOps engineer creating documentation for a junior developer.

**Context:**
Infrastructure as Code has been generated based on architectural analysis. Create a README.md that explains this infrastructure in a way that is:
- Technically accurate and sound
- Easy to understand for junior developers
- Specific to the actual architecture (not generic)
- Actionable with clear deployment instructions

**Architecture Recommendations:**

{architecture_recommendations}

**README Structure Requirements:**

Generate a complete README.md in markdown format with these sections:

1. **Header and Overview**
   - Title: "Infrastructure as Code - Architecture Documentation"
   - Include generation timestamp
   - Brief overview of what this infrastructure does (based on the architecture notes)

2. **Architecture Summary**
   - Summarize the key architectural decisions from the notes above
   - Explain the architecture pattern being used (e.g., microservices, serverless, etc.)
   - Describe how components interact (based on the actual resources identified)

3. **Cloud Providers**
   - List the cloud providers being used
   - Explain what resources are deployed on each provider

4. **Key Components**
   - List and explain the main infrastructure components (based on actual resource types)
   - For each component, explain its purpose in this specific architecture

5. **Prerequisites**
   - Terraform/OpenTofu installation (version 1.0+)
   - Cloud provider credentials (specific to the providers being used)
   - Required permissions (be specific based on the resources)

6. **Deployment Instructions**
   - Step-by-step guide to deploy this specific infrastructure
   - Include terraform init, plan, apply commands
   - Mention any provider-specific setup needed

7. **File Structure**
   - Explain the standard Terraform file organization
   - main.tf, variables.tf, outputs.tf, versions.tf, etc.

8. **Understanding the Code (For Junior Developers)**
   - Explain key Terraform/IaC concepts with examples from THIS architecture
   - Resources, Variables, Modules, Outputs
   - Use actual resource types from the architecture when giving examples

9. **Configuration**
   - Explain what variables can be customized
   - Provide guidance on common configuration scenarios

10. **Security Considerations**
    - Based on the architecture notes, highlight security best practices
    - IAM permissions, encryption, network isolation, etc.

11. **Operational Best Practices**
    - Version control, state management, workspace usage
    - Monitoring and logging recommendations

12. **Troubleshooting**
    - Common issues specific to this architecture
    - Provider-specific troubleshooting tips

13. **Cleanup**
    - How to destroy resources
    - Warnings about data loss

14. **Additional Resources**
    - Links to Terraform documentation
    - Cloud provider documentation
    - Relevant module documentation

**Important Guidelines:**
- Write in clear, professional markdown
- Use code blocks with proper syntax highlighting (```hcl, ```bash)
- Reference ACTUAL components from the architecture, not generic examples
- Explain WHY things are done, not just HOW
- Make it educational for junior developers while remaining technically accurate
- Include the current timestamp: {timestamp}

Generate the complete README.md content now.
