/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	cceanalyzer "github.com/appcd-dev/cce/pkg/analyzer"
	"github.com/appcd-dev/genie/pkg/analyzer"
	"github.com/appcd-dev/genie/pkg/codeowner"
	"github.com/appcd-dev/genie/pkg/granter"
	"github.com/appcd-dev/genie/pkg/iacgen/generator"
	"github.com/appcd-dev/genie/pkg/tui"
	"github.com/appcd-dev/go-lib/logger"
	"github.com/appcd-dev/go-lib/osutils"
	"github.com/spf13/cobra"
)

type grantCmdOption struct {
	SaveTo     string
	EnableChat bool

	// MapperFile is the path to a YAML file that contains additional definitions for the analyzer.
	MapperFile string
}

func (g grantCmdOption) validate() error {
	// Create the save-to directory if it doesn't exist
	err := os.MkdirAll(g.SaveTo, 0755)
	if err != nil {
		return fmt.Errorf("error creating the save-to directory: %w", err)
	}
	return nil
}

type grantCmd struct {
	opts       grantCmdOption
	rootOpts   *rootCmdOption
	granterSvc granter.Granter
	codeOwner  codeowner.CodeOwner
}

func NewGrantCommand(rootOpts *rootCmdOption) *grantCmd {
	return &grantCmd{
		rootOpts: rootOpts,
	}
}

func (g *grantCmd) command() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "grant",
		Short: "Interactive Intent-to-Infrastructure wizard",
		Long: `Grant your infrastructure wish! This command launches an interactive wizard
that helps you describe your application requirements and generates production-ready
infrastructure code using Stackgen's agentic intelligence.

Examples:
  genie grant`,
		RunE: g.run,
	}
	cwd, err := filepath.Abs(osutils.Getwd())
	if err != nil {
		return nil, fmt.Errorf("error getting the current working directory: %w", err)
	}

	// Bind flags to the grant command options
	cmd.Flags().StringVar(&g.rootOpts.codeDir, "code-dir", cwd, "code directory")
	cmd.Flags().StringVar(&g.opts.SaveTo, "save-to", filepath.Join(cwd, "genie_output"), "save to")
	cmd.Flags().BoolVar(&g.opts.EnableChat, "enable-chat", false, "enable chat")
	cmd.Flags().StringVar(&g.opts.MapperFile, "mapper-file", "", "Additional def yaml that contains method and resource mapping rules")

	return cmd, nil
}

// run executes the grant command logic
func (g *grantCmd) run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	var err error
	err = g.opts.validate()
	if err != nil {
		return err
	}

	// Initialize Services
	genieCfg, err := g.rootOpts.genieCfg()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	codeAnalyzer, err := analyzer.New(ctx, g.opts.MapperFile, cceanalyzer.New())
	if err != nil {
		return err
	}

	modelProvider := genieCfg.ModelConfig.NewEnvBasedModelProvider()

	llmBasedArchitect, err := generator.NewLLMBasedArchitect(ctx, modelProvider, genieCfg.Architect)
	if err != nil {
		return err
	}

	iacWriter, err := generator.NewLLMBasedIACWriter(ctx, modelProvider, genieCfg.Ops, genieCfg.SecOps)
	if err != nil {
		return err
	}

	g.granterSvc = granter.New(codeAnalyzer, llmBasedArchitect, iacWriter)
	if !g.opts.EnableChat {
		_, err := g.granterSvc.Generate(ctx, granter.GrantRequest{
			CodeDir: g.rootOpts.codeDir,
			SaveTo:  g.opts.SaveTo,
		})
		return err
	}
	g.codeOwner, err = codeowner.NewcodeOwner(ctx, modelProvider)
	if err != nil {
		return err
	}

	// Use TUI for interactive feedback during infrastructure generation
	// The granter will emit events through the event channel
	return tui.RunGrantWithTUI(ctx, g.grantWithTUI)
}

func (g *grantCmd) grantWithTUI(ctx context.Context, eventChan chan<- interface{}, userMessages <-chan string) error {
	logger.SetLogHandler(tui.SetupTUILogger(eventChan, slog.LevelDebug))
	// Run the granter workflow - it will emit events for progress
	_, err := g.granterSvc.Generate(ctx, granter.GrantRequest{
		CodeDir:   g.rootOpts.codeDir,
		SaveTo:    g.opts.SaveTo,
		EventChan: eventChan,
	})

	if err != nil {
		// Emit error event
		tui.EmitError(eventChan, err, "during infrastructure generation")
		return err
	}

	// Emit completion event
	tui.EmitCompletion(eventChan, true, fmt.Sprintf("🧞 Your infrastructure is ready at %s", g.opts.SaveTo), g.opts.SaveTo)

	// Chat Loop
	for {
		select {
		case input, ok := <-userMessages:
			if !ok {
				return nil
			}
			// Process input with ChatExpert
			outputChan := make(chan string)
			go func() {
				defer close(outputChan)
				if err := g.codeOwner.Chat(ctx, codeowner.CodeQuestion{
					Question:  input,
					OutputDir: g.opts.SaveTo,
					EventChan: eventChan,
				}, outputChan); err != nil {
					tui.EmitError(eventChan, err, "while processing chat message")
				}
			}()
			for response := range outputChan {
				tui.EmitAgentMessage(eventChan, "Genie", response)
			}

		case <-ctx.Done():
			return nil
		}
	}
}
