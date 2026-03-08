package modelprovider

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// openAICompletionModel implements model.Model for the v1/completions endpoint.
type openAICompletionModel struct {
	client *openai.Client
	name   string
}

// NewOpenAICompletionModel creates a new model that uses the OpenAI completions endpoint.
func NewOpenAICompletionModel(name string, opts ...option.RequestOption) model.Model {
	c := openai.NewClient(opts...)
	return &openAICompletionModel{
		client: &c,
		name:   name,
	}
}

func (m *openAICompletionModel) Info() model.Info {
	return model.Info{Name: m.name}
}

func (m *openAICompletionModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	if request == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	var prompt strings.Builder
	for _, msg := range request.Messages {
		prompt.WriteString(msg.Content)
		prompt.WriteString("\n")
	}

	body := openai.CompletionNewParams{
		Model:  openai.CompletionNewParamsModel(m.name),
		Prompt: openai.CompletionNewParamsPromptUnion{OfString: openai.String(prompt.String())},
	}
	if request.GenerationConfig.MaxTokens != nil {
		body.MaxTokens = openai.Int(int64(*request.GenerationConfig.MaxTokens))
	} else {
		// Default max tokens if not set to avoid minimal generation
		body.MaxTokens = openai.Int(1024)
	}
	if request.Temperature != nil {
		body.Temperature = openai.Float(float64(*request.Temperature))
	}
	if request.TopP != nil {
		body.TopP = openai.Float(float64(*request.TopP))
	}

	if len(request.Stop) > 0 {
		body.Stop = openai.CompletionNewParamsStopUnion{OfStringArray: request.Stop}
	}

	if request.Stream {
		ch := make(chan *model.Response, 100)
		go func() {
			defer close(ch)
			stream := m.client.Completions.NewStreaming(ctx, body)
			for stream.Next() {
				chunk := stream.Current()
				if len(chunk.Choices) > 0 {
					ch <- &model.Response{
						Choices: []model.Choice{{
							Message: model.Message{
								Role:    model.RoleAssistant,
								Content: chunk.Choices[0].Text,
							},
						}},
					}
				}
			}
			if err := stream.Err(); err != nil {
				ch <- &model.Response{Error: &model.ResponseError{Message: err.Error()}}
			}
		}()
		return ch, nil
	}

	completion, err := m.client.Completions.New(ctx, body)
	if err != nil {
		return nil, err
	}
	ch := make(chan *model.Response, 1)
	if len(completion.Choices) > 0 {
		ch <- &model.Response{
			Choices: []model.Choice{{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: completion.Choices[0].Text,
				},
			}},
		}
	} else {
		ch <- &model.Response{
			Choices: []model.Choice{{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: "",
				},
			}},
		}
	}
	close(ch)
	return ch, nil
}

type normalizeModel struct {
	model.Model
	modelName string
}

func (m *normalizeModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	if request != nil && isReasoningModel(m.modelName) {
		reqCopy := *request
		// Overwrite unsupported generation parameters for reasoning models.
		reqCopy.Temperature = nil
		reqCopy.TopP = nil
		reqCopy.PresencePenalty = nil
		reqCopy.FrequencyPenalty = nil
		return m.Model.GenerateContent(ctx, &reqCopy)
	}
	return m.Model.GenerateContent(ctx, request)
}

func isReasoningModel(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "o1") || strings.HasPrefix(n, "o3") || strings.HasPrefix(n, "o4") || strings.HasPrefix(n, "gpt-5")
}
