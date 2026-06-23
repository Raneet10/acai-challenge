package tools

import (
	"context"
	"log/slog"
	"sync"

	"github.com/openai/openai-go/v2"
)

// Tool bundles everything needed to expose a function-calling tool to OpenAI
// and handle the model's calls to it.
type Tool struct {
	Name        string
	Description string
	Parameters  openai.FunctionParameters // nil if the tool takes no arguments
	Handler     func(ctx context.Context, rawArgs string) string
	// Init, if set, runs once, concurrently with other tools' Init funcs, when
	// the registry is loaded (see Load). Tools without startup work leave this nil.
	Init func(ctx context.Context) error
}

func (t Tool) definition() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        t.Name,
		Description: openai.String(t.Description),
		Parameters:  t.Parameters,
	})
}

// Registry is an ordered collection of tools available to the assistant.
type Registry []Tool

func (r Registry) Definitions() []openai.ChatCompletionToolUnionParam {
	defs := make([]openai.ChatCompletionToolUnionParam, len(r))
	for i, t := range r {
		defs[i] = t.definition()
	}

	return defs
}

func (r Registry) Find(name string) (Tool, bool) {
	for _, t := range r {
		if t.Name == name {
			return t, true
		}
	}

	return Tool{}, false
}

// Load runs every tool's Init concurrently and waits for them all to finish.
// A failing Init is logged, not fatal — the registry is still usable, and
// individual tools fall back to their non-cached behavior if needed.
func Load(ctx context.Context, registry Registry) {
	var wg sync.WaitGroup

	for _, t := range registry {
		if t.Init == nil {
			continue
		}

		wg.Add(1)
		go func(t Tool) {
			defer wg.Done()

			if err := t.Init(ctx); err != nil {
				slog.ErrorContext(ctx, "Failed to initialize tool", "tool", t.Name, "error", err)
			}
		}(t)
	}

	wg.Wait()
	slog.InfoContext(ctx, "Tools loaded", "count", len(registry))
}
