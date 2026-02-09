package codeowner

import (
	"context"
	_ "embed"
	"strings"

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/langfuse"
	"github.com/appcd-dev/go-lib/logger"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool/file"
)

//go:embed prompts/persona.txt
var persona string

type CodeQuestion struct {
	Question  string
	EventChan chan<- interface{}
	OutputDir string
}

//go:generate go tool counterfeiter -generate

// CodeOwner is an expert that can chat about the codebase
//
//counterfeiter:generate . CodeOwner
type CodeOwner interface {
	Chat(ctx context.Context, req CodeQuestion, outputChan chan<- string) error
}

type codeOwner struct {
	expert expert.Expert
}

// NewcodeOwner creates a new codeOwner
func NewcodeOwner(ctx context.Context, modelProvider modelprovider.ModelProvider) (CodeOwner, error) {
	// Define Persona
	expertBio := expert.ExpertBio{
		Personality: langfuse.GetPrompt(ctx, "genie_codeowner_persona", persona),
		Name:        "code-owner",
		Description: "Code Owner that knows the in and out about the codebase",
	}

	exp, err := expertBio.ToExpert(ctx, modelProvider)
	if err != nil {
		return nil, err
	}

	// Pre-load context if available
	// (Optional: We could inject CCE analysis summary here if we wanted to prime the context)

	return &codeOwner{
		expert: exp,
	}, nil
}

func (c *codeOwner) Chat(ctx context.Context, req CodeQuestion, outputChan chan<- string) error {
	// Initialize standard file tools from trpc-agent-go
	logr := logger.GetLogger(ctx).With("fn", "codeExpert.Chat")
	toolSet, err := file.NewToolSet(file.WithBaseDir(req.OutputDir))
	if err != nil {
		logr.Error("failed to create file toolset", "err", err)
	}
	expertRequest := expert.Request{
		Message:      req.Question,
		EventChannel: req.EventChan,
		TaskType:     modelprovider.TaskToolCalling,
		ChoiceProcessor: func(choices ...model.Choice) {
			for i := range choices {
				if len(strings.TrimSpace(choices[i].Message.Content)) != 0 {
					logr.Debug("choice", "role", choices[i].Message.Role, "content", choices[i].Message.Content)
					if choices[i].Message.Role == model.RoleAssistant {
						outputChan <- choices[i].Message.Content
					}
				}
			}
		},
	}
	if toolSet != nil {
		expertRequest.AdditionalTools = toolSet.Tools(ctx)
	}

	_, err = c.expert.Do(ctx, expertRequest)
	return err
}
