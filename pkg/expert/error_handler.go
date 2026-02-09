package expert

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/go-lib/logger"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// HandleExpertError inspects errors returned from the expert runner.
// If the error is due to hitting the max tool iteration limit, it synthesizes
// a partial success response with an explanatory message.
// Otherwise, it returns the original error.
func HandleExpertError(err error) (Response, error) {
	if err == nil {
		return Response{}, nil
	}

	// Log the actual error for debugging
	logger.GetLogger(context.TODO()).Error("Expert error occurred", "error", err.Error(), "error_type", fmt.Sprintf("%T", err))

	// The runner returns a formatted error string when max tool iterations are exceeded.
	// See trpc-agent-go/internal/flow/processor/functioncall.go
	if strings.Contains(err.Error(), "max tool iterations") {
		return Response{
			Choices: []model.Choice{
				{
					Message: model.NewAssistantMessage("I stopped because I reached the maximum number of tool iterations I am allowed to make. I may have partially completed the task, but I could not finish it entirely."),
				},
			},
		}, nil
	}

	return Response{}, fmt.Errorf("failed to run the expert: %w", err)
}
