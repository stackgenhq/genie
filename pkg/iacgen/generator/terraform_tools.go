package generator

import (
	"github.com/appcd-dev/genie/pkg/tools/tftools"
	"github.com/sirupsen/logrus"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// TerraformTools provides access to Terraform registry tools wrapped for trpc-agent-go
type TerraformTools struct {
	registryTools tftools.MultiRegistryTools
}

// NewTerraformTools creates a new instance of Terraform tools with multi-registry support
func NewTerraformTools(maxPages int) *TerraformTools {
	logger := logrus.New()
	logger.SetLevel(logrus.FatalLevel)
	return &TerraformTools{
		registryTools: tftools.NewMultiRegistryTools(logger, maxPages),
	}
}

// GetTools returns all Terraform registry tools as trpc-agent-go compatible tools
// These tools support both Terraform and OpenTofu registries to avoid rate limits
func (t *TerraformTools) GetTools() []tool.Tool {
	return t.registryTools.GetTools()
}
