package chat

import (
	"context"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/acai-travel/tech-challenge/internal/chat/assistant"
	"github.com/acai-travel/tech-challenge/internal/chat/model"
	. "github.com/acai-travel/tech-challenge/internal/chat/testing"
	"github.com/acai-travel/tech-challenge/internal/chat/tools"
	"github.com/acai-travel/tech-challenge/internal/pb"
)

var weatherTempPattern = regexp.MustCompile(`(-?\d+(?:\.\d+)?)°C`)

func TestServer_StartConversation_Integration(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set; skipping integration test")
	}

	ctx := context.Background()

	t.Run("creates a real conversation against the live OpenAI API", WithFixture(func(t *testing.T, f *Fixture) {
		assist := assistant.New(tools.Default())
		srv := NewServer(f.Repository, assist)

		out, err := srv.StartConversation(ctx, &pb.StartConversationRequest{
			Message: "What's today's date? Answer in one short sentence.",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		t.Cleanup(func() {
			_ = f.Repository.DeleteConversation(ctx, out.GetConversationId())
		})

		if out.GetConversationId() == "" {
			t.Error("expected a conversation ID to be set")
		}

		if wantYear := time.Now().Format("2006"); !strings.Contains(out.GetReply(), wantYear) {
			t.Errorf("Reply = %q, expected it to mention the current year %q", out.GetReply(), wantYear)
		}

		if !strings.Contains(strings.ToLower(out.GetTitle()), "date") {
			t.Errorf("Title = %q, expected it to be about the date", out.GetTitle())
		}

		stored, err := f.Repository.DescribeConversation(ctx, out.GetConversationId())
		if err != nil {
			t.Fatalf("conversation was not persisted: %v", err)
		}

		if len(stored.Messages) != 2 {
			t.Errorf("expected 2 stored messages (user + assistant), got %d", len(stored.Messages))
		}
	}))

	t.Run("creates a real conversation that triggers the weather tool", WithFixture(func(t *testing.T, f *Fixture) {
		if os.Getenv("WEATHER_API_KEY") == "" {
			t.Skip("WEATHER_API_KEY not set; skipping weather integration test")
		}

		weather, err := tools.GetCurrentWeather(ctx, "Barcelona")
		if err != nil {
			t.Fatalf("failed to fetch reference weather: %v", err)
		}

		match := weatherTempPattern.FindStringSubmatch(weather)
		if match == nil {
			t.Fatalf("could not parse a temperature out of reference weather %q", weather)
		}

		tempFloat, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			t.Fatalf("could not parse temperature %q: %v", match[1], err)
		}
		wantTemp := strconv.Itoa(int(math.Round(tempFloat)))

		assist := assistant.New(tools.Default())
		srv := NewServer(f.Repository, assist)

		out, err := srv.StartConversation(ctx, &pb.StartConversationRequest{
			Message: "What's the weather like in Barcelona right now?",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		t.Cleanup(func() {
			_ = f.Repository.DeleteConversation(ctx, out.GetConversationId())
		})

		if !strings.Contains(out.GetReply(), wantTemp) {
			t.Errorf("Reply = %q, expected it to mention the current temperature %q (from %q)", out.GetReply(), wantTemp, weather)
		}

		title := strings.ToLower(out.GetTitle())
		if !strings.Contains(title, "weather") && !strings.Contains(title, "temperature") && !strings.Contains(title, "barcelona") {
			t.Errorf("Title = %q, expected it to be about the weather", out.GetTitle())
		}
	}))
}

func TestServer_ContinueConversation_Integration(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set; skipping integration test")
	}

	ctx := context.Background()

	t.Run("continues a real conversation, picking up context from StartConversation", WithFixture(func(t *testing.T, f *Fixture) {
		assist := assistant.New(tools.Default())
		srv := NewServer(f.Repository, assist)

		started, err := srv.StartConversation(ctx, &pb.StartConversationRequest{
			Message: "My favorite color is blue. Just acknowledge that in one short sentence.",
		})
		if err != nil {
			t.Fatalf("unexpected error starting conversation: %v", err)
		}

		t.Cleanup(func() {
			_ = f.Repository.DeleteConversation(ctx, started.GetConversationId())
		})

		// If ContinueConversation correctly picks up where StartConversation left
		// off, the model should be able to recall the color from the first turn
		// without it being repeated here.
		out, err := srv.ContinueConversation(ctx, &pb.ContinueConversationRequest{
			ConversationId: started.GetConversationId(),
			Message:        "What's my favorite color? Answer in one short sentence.",
		})
		if err != nil {
			t.Fatalf("unexpected error continuing conversation: %v", err)
		}

		if !strings.Contains(strings.ToLower(out.GetReply()), "blue") {
			t.Errorf("Reply = %q, expected it to recall the favorite color (blue) from the earlier turn", out.GetReply())
		}

		stored, err := f.Repository.DescribeConversation(ctx, started.GetConversationId())
		if err != nil {
			t.Fatalf("conversation was not persisted: %v", err)
		}

		if len(stored.Messages) != 4 {
			t.Fatalf("expected 4 stored messages (2 from start + 2 from continue), got %d", len(stored.Messages))
		}

		if got := stored.Messages[3]; got.Role != model.RoleAssistant || got.Content != out.GetReply() {
			t.Errorf("stored final message = %+v, want role=%s content=%q", got, model.RoleAssistant, out.GetReply())
		}
	}))
}
