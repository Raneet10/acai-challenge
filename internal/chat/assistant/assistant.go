package assistant

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/acai-travel/tech-challenge/internal/chat/model"
	"github.com/acai-travel/tech-challenge/internal/chat/tools"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

//go:generate go tool mockgen -destination=mock_completions_test.go -package=assistant github.com/acai-travel/tech-challenge/internal/chat/assistant completionsAPI

// completionsAPI is the subset of the OpenAI client used by Assistant. It's
// satisfied by *openai.ChatCompletionService, and abstracted here so tests
// can substitute a mock instead of calling the real OpenAI API.
type completionsAPI interface {
	New(ctx context.Context, body openai.ChatCompletionNewParams, opts ...option.RequestOption) (*openai.ChatCompletion, error)
}

const instrumentationName = "github.com/acai-travel/tech-challenge/internal/chat/assistant"

var tracer = otel.Tracer(instrumentationName)

var (
	operationDuration metric.Float64Histogram
	operationErrors   metric.Int64Counter
)

func init() {
	meter := otel.Meter(instrumentationName)

	var err error

	operationDuration, err = meter.Float64Histogram("assistant.operation.duration", metric.WithDescription("Duration of OpenAI calls and tool executions"), metric.WithUnit("s"))
	if err != nil {
		panic(err)
	}

	operationErrors, err = meter.Int64Counter("assistant.operation.errors", metric.WithDescription("Number of OpenAI calls that returned an error"))
	if err != nil {
		panic(err)
	}
}

// finishOperation records the duration (and, on failure, the error) of a
// traced operation, then ends its span. Call it right after the operation
// completes, with the span and start time from tracer.Start.
func finishOperation(ctx context.Context, span trace.Span, operation string, start time.Time, err error, extra ...attribute.KeyValue) {
	attrs := metric.WithAttributes(append([]attribute.KeyValue{attribute.String("operation", operation)}, extra...)...)
	operationDuration.Record(ctx, time.Since(start).Seconds(), attrs)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		operationErrors.Add(ctx, 1, attrs)
	}

	span.End()
}

type Assistant struct {
	completions completionsAPI
	tools       tools.Registry
}

func New(registry tools.Registry) *Assistant {
	cli := openai.NewClient()
	return &Assistant{completions: &cli.Chat.Completions, tools: registry}
}

func (a *Assistant) Title(ctx context.Context, conv *model.Conversation) (string, error) {
	if len(conv.Messages) == 0 {
		return "An empty conversation", nil
	}

	slog.InfoContext(ctx, "Generating title for conversation", "conversation_id", conv.ID)

	msgs := make([]openai.ChatCompletionMessageParamUnion, len(conv.Messages)+1)

	msgs[0] = openai.AssistantMessage("Generate a concise, descriptive title for the conversation based on the user message. The title should be a single line, no more than 80 characters, and should not include any special characters or emojis.")
	for i, m := range conv.Messages {
		msgs[i+1] = openai.UserMessage(m.Content)
	}

	completionsCtx, completionsSpan := tracer.Start(ctx, "openai.chat.completions")
	completionsStart := time.Now()
	resp, err := a.completions.New(completionsCtx, openai.ChatCompletionNewParams{
		Model:    openai.ChatModelO1,
		Messages: msgs,
	})
	finishOperation(completionsCtx, completionsSpan, "openai.chat.completions", completionsStart, err, attribute.String("model", string(openai.ChatModelO1)))

	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		return "", errors.New("empty response from OpenAI for title generation")
	}

	title := resp.Choices[0].Message.Content
	title = strings.ReplaceAll(title, "\n", " ")
	title = strings.Trim(title, " \t\r\n-\"'")

	if len(title) > 80 {
		title = title[:80]
	}

	return title, nil
}

func (a *Assistant) Reply(ctx context.Context, conv *model.Conversation) (string, error) {
	if len(conv.Messages) == 0 {
		return "", errors.New("conversation has no messages")
	}

	slog.InfoContext(ctx, "Generating reply for conversation", "conversation_id", conv.ID)

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("You are a helpful, concise AI assistant. Provide accurate, safe, and clear responses."),
	}

	for _, m := range conv.Messages {
		switch m.Role {
		case model.RoleUser:
			msgs = append(msgs, openai.UserMessage(m.Content))
		case model.RoleAssistant:
			msgs = append(msgs, openai.AssistantMessage(m.Content))
		}
	}

	for i := 0; i < 15; i++ {
		completionsCtx, completionsSpan := tracer.Start(ctx, "openai.chat.completions")
		completionsStart := time.Now()
		resp, err := a.completions.New(completionsCtx, openai.ChatCompletionNewParams{
			Model:    openai.ChatModelGPT4_1,
			Messages: msgs,
			Tools:    a.tools.Definitions(),
		})
		finishOperation(completionsCtx, completionsSpan, "openai.chat.completions", completionsStart, err, attribute.String("model", string(openai.ChatModelGPT4_1)))

		if err != nil {
			return "", err
		}

		if len(resp.Choices) == 0 {
			return "", errors.New("no choices returned by OpenAI")
		}

		if message := resp.Choices[0].Message; len(message.ToolCalls) > 0 {
			msgs = append(msgs, message.ToParam())

			for _, call := range message.ToolCalls {
				slog.InfoContext(ctx, "Tool call received", "name", call.Function.Name, "args", call.Function.Arguments)

				t, ok := a.tools.Find(call.Function.Name)
				if !ok {
					return "", errors.New("unknown tool call: " + call.Function.Name)
				}

				toolCtx, toolSpan := tracer.Start(ctx, "tool.execution", trace.WithAttributes(attribute.String("tool", call.Function.Name)))
				toolStart := time.Now()
				result, toolErr := t.Handler(toolCtx, call.Function.Arguments)
				finishOperation(toolCtx, toolSpan, "tool.execution", toolStart, toolErr, attribute.String("tool", call.Function.Name))

				if toolErr != nil {
					result = toolErr.Error()
				}

				msgs = append(msgs, openai.ToolMessage(result, call.ID))
			}

			continue
		}

		return resp.Choices[0].Message.Content, nil
	}

	return "", errors.New("too many tool calls, unable to generate reply")
}
