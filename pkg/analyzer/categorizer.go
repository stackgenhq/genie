package analyzer

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/go-lib/logger"
)

type CloudBasedPrompt struct {
	CloudProvider string
	Prompt        string
}

func (m MappedResources) Summarize(ctx context.Context) []CloudBasedPrompt {
	_ = getResourceCategorizer()
	logr := logger.GetLogger(ctx).With("fn", "MappedResources.Summarize")
	providerResources := m.GroupByProvider()
	logr.Info("Provider resources", "cloudProviderCount", len(providerResources))
	if len(providerResources) == 0 {
		return nil
	}
	result := []CloudBasedPrompt{}
	for cloudProvider, resource := range providerResources {
		logr.Info("Generating architecture design for cloud provider", "cloudProvider", cloudProvider, "identifiedResources", len(resource))
		notes := resource.generateGuideline(ctx, cloudProvider)
		logr.Info("architecture design phase complete", "cloudProvider", cloudProvider)
		result = append(result, CloudBasedPrompt{
			CloudProvider: cloudProvider,
			Prompt:        notes,
		})
	}
	return result
}

func (m MappedResources) generateGuideline(ctx context.Context, cloudProvider string) string {
	categorizer := getResourceCategorizer()

	categories := categorizer.categorizeResources(m)

	workflow := categories.inferWorkflow()

	return categories.buildStructuredPrompt(cloudProvider, workflow)
}

// buildStructuredPrompt creates a context-rich prompt following the "Perfect Prompt" pattern
func (categories ResourceCategories) buildStructuredPrompt(cloudProvider string, workflow string) string {
	var prompt strings.Builder

	// Role and Project Summary
	prompt.WriteString("**Role:** Senior Cloud Architect\n\n")
	prompt.WriteString(fmt.Sprintf("**Cloud Provider:** %s\n\n", cloudProvider))
	prompt.WriteString("**Project Summary:**\n")
	prompt.WriteString(fmt.Sprintf("I am analyzing a codebase that uses %d distinct architectural components across %d categories.\n\n",
		categories.len(), len(categories)))

	// Components Identified
	prompt.WriteString("**Components Identified:**\n\n")
	for category, resources := range categories {
		if len(resources) > 0 {
			prompt.WriteString(buildComponentSummary(category, resources))
		}
	}

	// Inferred Workflow
	prompt.WriteString("**Inferred Workflow:**\n\n")
	prompt.WriteString(workflow)
	prompt.WriteString("\n")

	// The Ask
	prompt.WriteString("**Architecture Recommendations Needed:**\n\n")
	prompt.WriteString("1. **Architecture Pattern**: What is the best architectural pattern for this system?\n")
	prompt.WriteString("   - Should this be Event-Driven, Microservices, Serverless, or a hybrid?\n")
	prompt.WriteString("   - Justify your recommendation based on the components detected.\n\n")

	prompt.WriteString("2. **Component Integration**: How should these components interact?\n")
	prompt.WriteString("   - Describe the data flow between services\n")
	prompt.WriteString("   - Recommend synchronous vs. asynchronous communication patterns\n")
	prompt.WriteString("   - Suggest error handling and retry strategies\n\n")

	prompt.WriteString("3. **Deployment Topology**: What is the optimal deployment approach?\n")
	prompt.WriteString("   - Serverless, Containerized (ECS/EKS), or Traditional (EC2)?\n")
	prompt.WriteString("   - Multi-AZ/Multi-Region considerations\n")
	prompt.WriteString("   - Network topology (VPC, subnets, security groups)\n\n")

	prompt.WriteString("4. **Security & IAM**: What are the critical security considerations?\n")
	prompt.WriteString("   - Minimum IAM permissions needed for each component\n")
	prompt.WriteString("   - Encryption requirements (at-rest and in-transit)\n")
	prompt.WriteString("   - Network isolation and access control\n\n")

	prompt.WriteString("5. **Operational Excellence**: How should this system be monitored and maintained?\n")
	prompt.WriteString("   - Logging and monitoring strategy\n")
	prompt.WriteString("   - Auto-scaling policies\n")
	prompt.WriteString("   - Disaster recovery and backup approach\n\n")

	prompt.WriteString("6. **Cost Optimization**: What are the cost considerations?\n")
	prompt.WriteString("   - Estimated cost drivers\n")
	prompt.WriteString("   - Recommendations for cost optimization\n\n")

	prompt.WriteString("Please provide a comprehensive architectural recommendation addressing all these points.\n")

	return prompt.String()
}
