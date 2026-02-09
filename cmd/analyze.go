package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	cceanalyzer "github.com/appcd-dev/cce/pkg/analyzer"
	"github.com/appcd-dev/genie/pkg/analyzer"
	"github.com/appcd-dev/go-lib/osutils"
	"github.com/spf13/cobra"
)

type analyzeCmd struct {
	rootOpts                *rootCmdOption
	cceOutputPath           string
	customMappingDefinition string
}

func newAnalyzeCommand(rootOpts *rootCmdOption) *analyzeCmd {
	cmd := &analyzeCmd{
		rootOpts:      rootOpts,
		cceOutputPath: filepath.Join(osutils.Getwd(), "cce.json"),
	}

	customDefintion := filepath.Join(osutils.Getwd(), "cce.json")
	if _, err := os.Stat(customDefintion); err == nil {
		cmd.customMappingDefinition = customDefintion
	}
	return cmd
}

func (a *analyzeCmd) command() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "analyze",
		Short:  "Analyze code to generate Code Context Engine (CCE) report",
		Long:   "Analyze code to generate a Code Context Engine (CCE) report that shows how the code interacts with cloud provider resources",
		Hidden: true,
		RunE:   a.run,
	}
	cmd.Flags().StringVarP(&a.cceOutputPath, "cce-output", "o", a.cceOutputPath, "Path to output CCE report")
	cmd.Flags().StringVarP(&a.customMappingDefinition, "mapper-file", "c", a.customMappingDefinition, "Path to custom definition YAML")
	return cmd
}

func (a *analyzeCmd) run(cmd *cobra.Command, args []string) error {
	codeAnalyzer, err := analyzer.New(cmd.Context(), a.customMappingDefinition, cceanalyzer.New())
	if err != nil {
		return err
	}
	result, err := codeAnalyzer.Analyze(cmd.Context(), analyzer.AnalysisInput{
		Path:          a.rootOpts.codeDir,
		SaveCCEJSONTo: a.cceOutputPath,
	})
	if err != nil {
		return err
	}
	fmt.Println(result.Summarize(cmd.Context()))
	return nil
}
