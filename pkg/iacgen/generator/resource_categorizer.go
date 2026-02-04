package generator

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/appcd-dev/genie/pkg/analyzer"
	"gopkg.in/yaml.v3"
)

//go:embed resource_guide.yaml
var resourceGuideYAML []byte

// ResourceCategory represents the architectural tier of a resource
type ResourceCategory string

func (r ResourceCategory) String() string {
	return string(r)
}

const (
	CategoryStorage    ResourceCategory = "Storage"
	CategoryCompute    ResourceCategory = "Compute"
	CategoryMessaging  ResourceCategory = "Messaging"
	CategoryAIML       ResourceCategory = "AI/ML"
	CategoryNetworking ResourceCategory = "Networking"
	CategoryDatabase   ResourceCategory = "Database"
	CategorySecurity   ResourceCategory = "Security"
	CategoryMonitoring ResourceCategory = "Monitoring"
	CategoryOther      ResourceCategory = "Other"
)

// PromptOptions configures how component summaries are generated
type PromptOptions struct {
	MaxResourcesPerCategory int  // Maximum resources to include per category (0 = unlimited)
	IncludeUsageLocations   bool // Include file:line locations (default: false for cost savings)
	AggregateByFamily       bool // Aggregate related resources by service family
}

// DefaultPromptOptions returns cost-optimized defaults
func DefaultPromptOptions() PromptOptions {
	return PromptOptions{
		MaxResourcesPerCategory: 10,
		IncludeUsageLocations:   false,
		AggregateByFamily:       true,
	}
}

// awsServiceFamilies maps specific resource names to their service family for aggregation
var awsServiceFamilies = map[string]string{
	// S3 family
	"s3": "S3 Storage", "s3control": "S3 Storage", "s3outposts": "S3 Storage",
	// EC2 family
	"ec2": "EC2 Compute", "ebs": "EC2 Compute", "autoscaling": "EC2 Compute",
	// Lambda family
	"lambda": "Lambda Functions",
	// DynamoDB family
	"dynamodb": "DynamoDB", "dynamodbstreams": "DynamoDB",
	// RDS family
	"rds": "RDS Database", "aurora": "RDS Database",
	// IAM family
	"iam": "IAM", "sts": "IAM",
	// Messaging
	"sqs": "SQS Queues", "sns": "SNS Topics", "eventbridge": "EventBridge",
	// Security
	"kms": "KMS Encryption", "secretsmanager": "Secrets Manager",
}

// getServiceFamily returns the service family for a resource, or the resource name if no family
func getServiceFamily(resourceName string) string {
	lower := strings.ToLower(resourceName)
	// Try exact match first
	if family, ok := awsServiceFamilies[lower]; ok {
		return family
	}
	// Try prefix matching for resources like "s3_bucket" → "s3"
	for prefix, family := range awsServiceFamilies {
		if strings.HasPrefix(lower, prefix+"_") || strings.HasPrefix(lower, prefix) {
			return family
		}
	}
	return resourceName
}

// ResourceCategories maps categories to their resources
type ResourceCategories map[ResourceCategory]analyzer.MappedResources

func (r ResourceCategories) Keys() []string {
	result := make([]string, 0, len(r))
	for i := range r {
		result = append(result, i.String())
	}
	return result
}

// resourceGuide holds the parsed YAML configuration
type resourceGuide struct {
	Categories map[string]categoryDefinition `yaml:"categories"`
}

type categoryDefinition struct {
	Keywords []string `yaml:"keywords"`
	AWS      []string `yaml:"aws"`
	Azure    []string `yaml:"azure"`
	GCP      []string `yaml:"gcp"`
}

// resourceCategorizer provides fast lookup for resource categorization
type resourceCategorizer struct {
	guide          resourceGuide
	keywordLookup  map[string]ResourceCategory
	resourceLookup map[string]ResourceCategory
}

var (
	categorizer     *resourceCategorizer
	categorizerOnce sync.Once
)

// getResourceCategorizer returns a singleton instance of the categorizer
func getResourceCategorizer() *resourceCategorizer {
	categorizerOnce.Do(func() {
		var guide resourceGuide
		if err := yaml.Unmarshal(resourceGuideYAML, &guide); err != nil {
			// Fallback to basic categorization if YAML parsing fails
			categorizer = &resourceCategorizer{
				keywordLookup:  make(map[string]ResourceCategory),
				resourceLookup: make(map[string]ResourceCategory),
			}
			return
		}

		categorizer = &resourceCategorizer{
			guide:          guide,
			keywordLookup:  make(map[string]ResourceCategory),
			resourceLookup: make(map[string]ResourceCategory),
		}

		// Build lookup maps for O(1) access
		for categoryName, categoryDef := range guide.Categories {
			category := ResourceCategory(categoryName)

			// Index keywords
			for _, keyword := range categoryDef.Keywords {
				categorizer.keywordLookup[strings.ToLower(keyword)] = category
			}

			// Index specific cloud resources
			for _, resource := range categoryDef.AWS {
				categorizer.resourceLookup[strings.ToLower(resource)] = category
			}
			for _, resource := range categoryDef.Azure {
				categorizer.resourceLookup[strings.ToLower(resource)] = category
			}
			for _, resource := range categoryDef.GCP {
				categorizer.resourceLookup[strings.ToLower(resource)] = category
			}
		}
	})

	return categorizer
}

// categorizeResources groups resources by their architectural tier
func (c *resourceCategorizer) categorizeResources(resources analyzer.MappedResources) ResourceCategories {
	categories := make(ResourceCategories)

	for _, resource := range resources {
		category := c.determineCategory(resource.MappedResource.Resource)
		categories[category] = append(categories[category], resource)
	}

	return categories
}

// determineCategory maps a resource type to its architectural category using fast lookups
func (c *resourceCategorizer) determineCategory(resourceType string) ResourceCategory {
	resourceLower := strings.ToLower(resourceType)

	// First, try exact match on resource name
	if category, found := c.resourceLookup[resourceLower]; found {
		return category
	}

	// Then, try keyword matching
	for keyword, category := range c.keywordLookup {
		if strings.Contains(resourceLower, keyword) {
			return category
		}
	}

	return CategoryOther
}

// inferWorkflow analyzes resource categories to detect common architectural patterns
func (categories ResourceCategories) inferWorkflow() string {
	var parts []string

	// Detect and collect each pattern
	if pattern := categories.detectEventDrivenPattern(); pattern != "" {
		parts = append(parts, pattern)
	}
	if pattern := categories.detectAIMLPipelinePattern(); pattern != "" {
		parts = append(parts, pattern)
	}
	if pattern := categories.detectAPIBackendPattern(); pattern != "" {
		parts = append(parts, pattern)
	}
	if pattern := categories.detectStreamProcessingPattern(); pattern != "" {
		parts = append(parts, pattern)
	}
	if dataFlow := categories.inferDataFlow(); dataFlow != "" {
		parts = append(parts, dataFlow)
	}

	// Fallback if no patterns detected
	if len(parts) == 0 {
		return "**Custom Architecture Pattern:**\n" +
			"Multiple resource types detected. Workflow requires analysis.\n"
	}

	return strings.Join(parts, "")
}

// detectEventDrivenPattern checks for event-driven architecture
func (categories ResourceCategories) detectEventDrivenPattern() string {
	hasMessaging := len(categories[CategoryMessaging]) > 0
	hasCompute := len(categories[CategoryCompute]) > 0
	hasStorage := len(categories[CategoryStorage]) > 0
	hasDatabase := len(categories[CategoryDatabase]) > 0

	if !hasMessaging || !hasCompute && !hasStorage && !hasDatabase {
		return ""
	}

	var result strings.Builder
	result.WriteString("**Event-Driven Architecture Pattern Detected:**\n")
	result.WriteString("- Components communicate via messaging/events\n")
	if hasStorage {
		result.WriteString("- Storage operations may trigger events\n")
	}
	if hasCompute {
		result.WriteString("- Compute functions likely process events asynchronously\n")
	}
	result.WriteString("\n")
	return result.String()
}

// detectAIMLPipelinePattern checks for AI/ML pipeline architecture
func (categories ResourceCategories) detectAIMLPipelinePattern() string {
	hasAIML := len(categories[CategoryAIML]) > 0
	hasStorage := len(categories[CategoryStorage]) > 0
	hasMessaging := len(categories[CategoryMessaging]) > 0

	if !hasAIML || !hasStorage {
		return ""
	}

	var result strings.Builder
	result.WriteString("**AI/ML Pipeline Pattern Detected:**\n")
	result.WriteString("- Data flows from storage to AI/ML services\n")
	if hasMessaging {
		result.WriteString("- Results published to messaging system for downstream processing\n")
	}
	result.WriteString("\n")
	return result.String()
}

// detectAPIBackendPattern checks for API backend architecture
func (categories ResourceCategories) detectAPIBackendPattern() string {
	hasNetworking := len(categories[CategoryNetworking]) > 0
	hasCompute := len(categories[CategoryCompute]) > 0
	hasDatabase := len(categories[CategoryDatabase]) > 0

	if !hasNetworking || !hasCompute && !hasDatabase {
		return ""
	}

	var result strings.Builder
	result.WriteString("**API Backend Pattern Detected:**\n")
	result.WriteString("- API Gateway/Load Balancer handles incoming requests\n")
	if hasCompute {
		result.WriteString("- Compute layer processes business logic\n")
	}
	if hasDatabase {
		result.WriteString("- Database layer persists application state\n")
	}
	result.WriteString("\n")
	return result.String()
}

// detectStreamProcessingPattern checks for stream processing architecture
func (categories ResourceCategories) detectStreamProcessingPattern() string {
	hasMessaging := len(categories[CategoryMessaging]) > 0
	hasCompute := len(categories[CategoryCompute]) > 0
	hasKinesis := strings.Contains(
		strings.ToLower(getCategoryResourceNames(categories[CategoryMessaging])),
		"kinesis",
	)

	if !hasMessaging || !hasCompute || !hasKinesis {
		return ""
	}

	return "**Stream Processing Pattern Detected:**\n" +
		"- Real-time data streaming with Kinesis\n" +
		"- Compute functions process stream records\n\n"
}

// inferDataFlow infers the likely data flow based on detected components
func (categories ResourceCategories) inferDataFlow() string {
	hasStorage := len(categories[CategoryStorage]) > 0
	hasAIML := len(categories[CategoryAIML]) > 0
	hasMessaging := len(categories[CategoryMessaging]) > 0
	hasNetworking := len(categories[CategoryNetworking]) > 0
	hasCompute := len(categories[CategoryCompute]) > 0
	hasDatabase := len(categories[CategoryDatabase]) > 0

	if hasStorage && hasAIML && hasMessaging {
		return "**Likely Data Flow:**\n" +
			"1. Data stored in storage layer (S3/Blob/GCS)\n" +
			"2. AI/ML service analyzes the data (Bedrock/SageMaker/VertexAI)\n" +
			"3. Results published to messaging system (EventBridge/EventGrid/PubSub)\n" +
			"4. Downstream consumers process results\n"
	}

	if hasNetworking && hasCompute && hasDatabase {
		return "**Likely Data Flow:**\n" +
			"1. Requests arrive via API Gateway/Load Balancer\n" +
			"2. Compute layer processes requests\n" +
			"3. Data persisted to database\n" +
			"4. Response returned to client\n"
	}

	return ""
}

// buildComponentSummary creates a concise summary of resources in a category (legacy, uses default options)
func buildComponentSummary(category ResourceCategory, resources analyzer.MappedResources) string {
	return buildCompactComponentSummary(category, resources, DefaultPromptOptions())
}

// buildCompactComponentSummary creates a token-optimized summary of resources in a category
func buildCompactComponentSummary(category ResourceCategory, resources analyzer.MappedResources, opts PromptOptions) string {
	var summary strings.Builder

	summary.WriteString(fmt.Sprintf("### %s Components\n\n", category))

	// Aggregate resources - either by service family or by resource type
	aggregated := make(map[string]int)
	for _, resource := range resources {
		key := resource.MappedResource.Resource
		if opts.AggregateByFamily {
			key = getServiceFamily(key)
		}
		aggregated[key]++
	}

	// Sort by usage count (descending) for consistent output
	type resourceCount struct {
		name  string
		count int
	}
	sorted := make([]resourceCount, 0, len(aggregated))
	for name, count := range aggregated {
		sorted = append(sorted, resourceCount{name, count})
	}
	// Sort by count descending, then by name for stability
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].count > sorted[i].count ||
				(sorted[j].count == sorted[i].count && sorted[j].name < sorted[i].name) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Apply top N limiting
	max := len(sorted)
	truncated := false
	if opts.MaxResourcesPerCategory > 0 && max > opts.MaxResourcesPerCategory {
		max = opts.MaxResourcesPerCategory
		truncated = true
	}

	// Generate compact summary - just resource names and counts
	for i := 0; i < max; i++ {
		summary.WriteString(fmt.Sprintf("- **%s** (%d usage(s))\n", sorted[i].name, sorted[i].count))
	}

	if truncated {
		remainingCount := 0
		for i := max; i < len(sorted); i++ {
			remainingCount += sorted[i].count
		}
		summary.WriteString(fmt.Sprintf("- *...and %d more resources (%d usage(s))*\n", len(sorted)-max, remainingCount))
	}

	summary.WriteString("\n")
	return summary.String()
}

// getCategoryResourceNames returns a concatenated string of all resource names in a category
func getCategoryResourceNames(resources analyzer.MappedResources) string {
	var names []string
	for _, resource := range resources {
		names = append(names, resource.MappedResource.Resource)
	}
	return strings.Join(names, ", ")
}
