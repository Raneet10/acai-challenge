package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/acai-travel/tech-challenge/internal/chat/model"
	"github.com/acai-travel/tech-challenge/internal/chat/tools"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"go.uber.org/mock/gomock"
)

func mustChatCompletion(t *testing.T, body string) *openai.ChatCompletion {
	t.Helper()

	var resp openai.ChatCompletion
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("failed to unmarshal fixture chat completion: %v", err)
	}

	return &resp
}

func TestAssistant_Title(t *testing.T) {
	ctx := context.Background()

	t.Run("returns a default title for an empty conversation", func(t *testing.T) {
		a := &Assistant{completions: NewMockcompletionsAPI(gomock.NewController(t))}

		got, err := a.Title(ctx, &model.Conversation{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if want := "An empty conversation"; got != want {
			t.Errorf("Title() = %q, want %q", got, want)
		}
	})

	t.Run("generates and cleans up a title from the model", func(t *testing.T) {
		conv := &model.Conversation{Messages: []*model.Message{{Content: "What's the weather in Barcelona?"}}}

		completions := NewMockcompletionsAPI(gomock.NewController(t))
		completions.EXPECT().New(ctx, gomock.Any()).DoAndReturn(
			func(_ context.Context, body openai.ChatCompletionNewParams, _ ...option.RequestOption) (*openai.ChatCompletion, error) {
				if body.Model != openai.ChatModelO1 {
					t.Errorf("Model = %q, want %q", body.Model, openai.ChatModelO1)
				}

				if len(body.Messages) != len(conv.Messages)+1 {
					t.Errorf("len(Messages) = %d, want %d", len(body.Messages), len(conv.Messages)+1)
				}

				return mustChatCompletion(t, `{"choices":[{"message":{"role":"assistant","content":"\n\"Weather in Barcelona\"\n"}}]}`), nil
			})

		a := &Assistant{completions: completions}

		got, err := a.Title(ctx, conv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if want := "Weather in Barcelona"; got != want {
			t.Errorf("Title() = %q, want %q", got, want)
		}
	})

	t.Run("truncates titles longer than 80 characters", func(t *testing.T) {
		conv := &model.Conversation{Messages: []*model.Message{{Content: "Hello"}}}
		long := strings.Repeat("a", 100)

		completions := NewMockcompletionsAPI(gomock.NewController(t))
		completions.EXPECT().New(ctx, gomock.Any()).Return(
			mustChatCompletion(t, fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":%q}}]}`, long)), nil)

		a := &Assistant{completions: completions}

		got, err := a.Title(ctx, conv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(got) != 80 {
			t.Errorf("len(Title()) = %d, want 80", len(got))
		}
	})

	t.Run("returns an error when the response has no choices", func(t *testing.T) {
		conv := &model.Conversation{Messages: []*model.Message{{Content: "Hello"}}}

		completions := NewMockcompletionsAPI(gomock.NewController(t))
		completions.EXPECT().New(ctx, gomock.Any()).Return(&openai.ChatCompletion{}, nil)

		a := &Assistant{completions: completions}

		if _, err := a.Title(ctx, conv); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("propagates an error from the API call", func(t *testing.T) {
		conv := &model.Conversation{Messages: []*model.Message{{Content: "Hello"}}}

		completions := NewMockcompletionsAPI(gomock.NewController(t))
		completions.EXPECT().New(ctx, gomock.Any()).Return(nil, errors.New("openai is down"))

		a := &Assistant{completions: completions}

		if _, err := a.Title(ctx, conv); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})
}

func TestAssistant_Reply(t *testing.T) {
	ctx := context.Background()

	t.Run("returns an error for an empty conversation", func(t *testing.T) {
		a := &Assistant{completions: NewMockcompletionsAPI(gomock.NewController(t))}

		if _, err := a.Reply(ctx, &model.Conversation{}); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("returns the model's reply directly when no tools are called", func(t *testing.T) {
		conv := &model.Conversation{Messages: []*model.Message{{Role: model.RoleUser, Content: "Hello"}}}

		completions := NewMockcompletionsAPI(gomock.NewController(t))
		completions.EXPECT().New(ctx, gomock.Any()).Return(
			mustChatCompletion(t, `{"choices":[{"message":{"role":"assistant","content":"Hi there!"}}]}`), nil)

		a := &Assistant{completions: completions}

		got, err := a.Reply(ctx, conv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if want := "Hi there!"; got != want {
			t.Errorf("Reply() = %q, want %q", got, want)
		}
	})

	t.Run("executes a tool call and feeds the result back to the model", func(t *testing.T) {
		conv := &model.Conversation{Messages: []*model.Message{{Role: model.RoleUser, Content: "What's 2+2?"}}}

		var gotArgs string
		registry := tools.Registry{{
			Name: "test_tool",
			Handler: func(_ context.Context, rawArgs string) string {
				gotArgs = rawArgs
				return "4"
			},
		}}

		toolCallResp := mustChatCompletion(t, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"test_tool","arguments":"{\"a\":2,\"b\":2}"}}]}}]}`)
		finalResp := mustChatCompletion(t, `{"choices":[{"message":{"role":"assistant","content":"It's 4."}}]}`)

		completions := NewMockcompletionsAPI(gomock.NewController(t))
		completions.EXPECT().New(ctx, gomock.Any()).Return(toolCallResp, nil)
		completions.EXPECT().New(ctx, gomock.Any()).Return(finalResp, nil)

		a := &Assistant{completions: completions, tools: registry}

		got, err := a.Reply(ctx, conv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if want := "It's 4."; got != want {
			t.Errorf("Reply() = %q, want %q", got, want)
		}

		if want := `{"a":2,"b":2}`; gotArgs != want {
			t.Errorf("tool handler called with args = %q, want %q", gotArgs, want)
		}
	})

	t.Run("returns an error for an unknown tool call", func(t *testing.T) {
		conv := &model.Conversation{Messages: []*model.Message{{Role: model.RoleUser, Content: "Hello"}}}

		resp := mustChatCompletion(t, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unknown_tool","arguments":"{}"}}]}}]}`)

		completions := NewMockcompletionsAPI(gomock.NewController(t))
		completions.EXPECT().New(ctx, gomock.Any()).Return(resp, nil)

		a := &Assistant{completions: completions}

		if _, err := a.Reply(ctx, conv); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("gives up after too many tool call rounds", func(t *testing.T) {
		conv := &model.Conversation{Messages: []*model.Message{{Role: model.RoleUser, Content: "Hello"}}}

		registry := tools.Registry{{
			Name:    "loop_tool",
			Handler: func(context.Context, string) string { return "ok" },
		}}

		resp := mustChatCompletion(t, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"loop_tool","arguments":"{}"}}]}}]}`)

		completions := NewMockcompletionsAPI(gomock.NewController(t))
		completions.EXPECT().New(ctx, gomock.Any()).Return(resp, nil).Times(15)

		a := &Assistant{completions: completions, tools: registry}

		if _, err := a.Reply(ctx, conv); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("returns an error when the response has no choices", func(t *testing.T) {
		conv := &model.Conversation{Messages: []*model.Message{{Role: model.RoleUser, Content: "Hello"}}}

		completions := NewMockcompletionsAPI(gomock.NewController(t))
		completions.EXPECT().New(ctx, gomock.Any()).Return(&openai.ChatCompletion{}, nil)

		a := &Assistant{completions: completions}

		if _, err := a.Reply(ctx, conv); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("propagates an error from the API call", func(t *testing.T) {
		conv := &model.Conversation{Messages: []*model.Message{{Role: model.RoleUser, Content: "Hello"}}}

		completions := NewMockcompletionsAPI(gomock.NewController(t))
		completions.EXPECT().New(ctx, gomock.Any()).Return(nil, errors.New("openai is down"))

		a := &Assistant{completions: completions}

		if _, err := a.Reply(ctx, conv); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})
}
