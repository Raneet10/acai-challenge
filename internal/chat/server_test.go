package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/acai-travel/tech-challenge/internal/chat/model"
	. "github.com/acai-travel/tech-challenge/internal/chat/testing"
	"github.com/acai-travel/tech-challenge/internal/pb"
	"github.com/google/go-cmp/cmp"
	"github.com/twitchtv/twirp"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestServer_DescribeConversation(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(model.New(ConnectMongo()), nil)

	t.Run("describe existing conversation", WithFixture(func(t *testing.T, f *Fixture) {
		c := f.CreateConversation()

		out, err := srv.DescribeConversation(ctx, &pb.DescribeConversationRequest{ConversationId: c.ID.Hex()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got, want := out.GetConversation(), c.Proto()
		if !cmp.Equal(got, want, protocmp.Transform()) {
			t.Errorf("DescribeConversation() mismatch (-got +want):\n%s", cmp.Diff(got, want, protocmp.Transform()))
		}
	}))

	t.Run("describe non existing conversation should return 404", WithFixture(func(t *testing.T, f *Fixture) {
		_, err := srv.DescribeConversation(ctx, &pb.DescribeConversationRequest{ConversationId: "08a59244257c872c5943e2a2"})
		if err == nil {
			t.Fatal("expected error for non-existing conversation, got nil")
		}

		if te, ok := err.(twirp.Error); !ok || te.Code() != twirp.NotFound {
			t.Fatalf("expected twirp.NotFound error, got %v", err)
		}
	}))
}

func TestServer_StartConversation(t *testing.T) {
	ctx := context.Background()

	t.Run("creates a conversation, sets the title, and triggers the assistant's reply", WithFixture(func(t *testing.T, f *Fixture) {
		const (
			wantMessage = "What's the weather in Barcelona?"
			wantTitle   = "Weather in Barcelona"
			wantReply   = "It's sunny and 22°C in Barcelona."
		)

		assist := NewMockAssistant(gomock.NewController(t))
		assist.EXPECT().Title(gomock.Any(), gomock.Any()).Return(wantTitle, nil)
		assist.EXPECT().Reply(gomock.Any(), gomock.Any()).Return(wantReply, nil)

		srv := NewServer(f.Repository, assist)

		out, err := srv.StartConversation(ctx, &pb.StartConversationRequest{Message: wantMessage})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		t.Cleanup(func() {
			_ = f.Repository.DeleteConversation(ctx, out.GetConversationId())
		})

		if out.GetConversationId() == "" {
			t.Error("expected a conversation ID to be set")
		}

		if out.GetTitle() != wantTitle {
			t.Errorf("Title = %q, want %q", out.GetTitle(), wantTitle)
		}

		if out.GetReply() != wantReply {
			t.Errorf("Reply = %q, want %q", out.GetReply(), wantReply)
		}

		stored, err := f.Repository.DescribeConversation(ctx, out.GetConversationId())
		if err != nil {
			t.Fatalf("conversation was not persisted: %v", err)
		}

		if stored.Title != wantTitle {
			t.Errorf("stored title = %q, want %q", stored.Title, wantTitle)
		}

		if len(stored.Messages) != 2 {
			t.Fatalf("expected 2 stored messages (user + assistant), got %d", len(stored.Messages))
		}

		if got := stored.Messages[0]; got.Role != model.RoleUser || got.Content != wantMessage {
			t.Errorf("first message = %+v, want role=%s content=%q", got, model.RoleUser, wantMessage)
		}

		if got := stored.Messages[1]; got.Role != model.RoleAssistant || got.Content != wantReply {
			t.Errorf("second message = %+v, want role=%s content=%q", got, model.RoleAssistant, wantReply)
		}
	}))

	t.Run("falls back to the default title when Title fails", WithFixture(func(t *testing.T, f *Fixture) {
		assist := NewMockAssistant(gomock.NewController(t))
		assist.EXPECT().Title(gomock.Any(), gomock.Any()).Return("", errors.New("boom"))
		assist.EXPECT().Reply(gomock.Any(), gomock.Any()).Return("ok", nil)

		srv := NewServer(f.Repository, assist)

		out, err := srv.StartConversation(ctx, &pb.StartConversationRequest{Message: "Hello"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		t.Cleanup(func() {
			_ = f.Repository.DeleteConversation(ctx, out.GetConversationId())
		})

		const wantTitle = "Untitled conversation"
		if out.GetTitle() != wantTitle {
			t.Errorf("Title = %q, want fallback %q", out.GetTitle(), wantTitle)
		}
	}))

	t.Run("requires a non-empty message", WithFixture(func(t *testing.T, f *Fixture) {
		// No EXPECT() calls: the mock fails the test if Title/Reply are
		// invoked before the message is validated.
		assist := NewMockAssistant(gomock.NewController(t))
		srv := NewServer(f.Repository, assist)

		_, err := srv.StartConversation(ctx, &pb.StartConversationRequest{Message: "   "})
		if err == nil {
			t.Fatal("expected error for empty message, got nil")
		}

		if te, ok := err.(twirp.Error); !ok || te.Code() != twirp.InvalidArgument {
			t.Fatalf("expected twirp.InvalidArgument error, got %v", err)
		}
	}))
}

func TestServer_ContinueConversation(t *testing.T) {
	ctx := context.Background()

	t.Run("continues an existing conversation and triggers the assistant's reply", WithFixture(func(t *testing.T, f *Fixture) {
		c := f.CreateConversation()

		const (
			wantMessage = "Is it still sunny?"
			wantReply   = "Still sunny."
		)

		assist := NewMockAssistant(gomock.NewController(t))
		assist.EXPECT().Reply(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, conv *model.Conversation) (string, error) {
			if len(conv.Messages) != 2 {
				t.Errorf("Reply() called with %d messages, want 2", len(conv.Messages))
			}
			return wantReply, nil
		})

		srv := NewServer(f.Repository, assist)

		out, err := srv.ContinueConversation(ctx, &pb.ContinueConversationRequest{
			ConversationId: c.ID.Hex(),
			Message:        wantMessage,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if out.GetReply() != wantReply {
			t.Errorf("Reply = %q, want %q", out.GetReply(), wantReply)
		}

		stored, err := f.Repository.DescribeConversation(ctx, c.ID.Hex())
		if err != nil {
			t.Fatalf("failed to load stored conversation: %v", err)
		}

		if len(stored.Messages) != 3 {
			t.Fatalf("expected 3 stored messages, got %d", len(stored.Messages))
		}

		if got := stored.Messages[1]; got.Role != model.RoleUser || got.Content != wantMessage {
			t.Errorf("second message = %+v, want role=%s content=%q", got, model.RoleUser, wantMessage)
		}

		if got := stored.Messages[2]; got.Role != model.RoleAssistant || got.Content != wantReply {
			t.Errorf("third message = %+v, want role=%s content=%q", got, model.RoleAssistant, wantReply)
		}
	}))

	t.Run("requires a conversation ID", WithFixture(func(t *testing.T, f *Fixture) {
		assist := NewMockAssistant(gomock.NewController(t))
		srv := NewServer(f.Repository, assist)

		_, err := srv.ContinueConversation(ctx, &pb.ContinueConversationRequest{Message: "Hello"})
		if err == nil {
			t.Fatal("expected error for missing conversation ID, got nil")
		}

		if te, ok := err.(twirp.Error); !ok || te.Code() != twirp.InvalidArgument {
			t.Fatalf("expected twirp.InvalidArgument error, got %v", err)
		}
	}))

	t.Run("requires a non-empty message", WithFixture(func(t *testing.T, f *Fixture) {
		c := f.CreateConversation()

		assist := NewMockAssistant(gomock.NewController(t))
		srv := NewServer(f.Repository, assist)

		_, err := srv.ContinueConversation(ctx, &pb.ContinueConversationRequest{ConversationId: c.ID.Hex(), Message: "   "})
		if err == nil {
			t.Fatal("expected error for empty message, got nil")
		}

		if te, ok := err.(twirp.Error); !ok || te.Code() != twirp.InvalidArgument {
			t.Fatalf("expected twirp.InvalidArgument error, got %v", err)
		}
	}))

	t.Run("returns 404 for a non-existing conversation", WithFixture(func(t *testing.T, f *Fixture) {
		assist := NewMockAssistant(gomock.NewController(t))
		srv := NewServer(f.Repository, assist)

		_, err := srv.ContinueConversation(ctx, &pb.ContinueConversationRequest{
			ConversationId: "08a59244257c872c5943e2a2",
			Message:        "Hello",
		})
		if err == nil {
			t.Fatal("expected error for non-existing conversation, got nil")
		}

		if te, ok := err.(twirp.Error); !ok || te.Code() != twirp.NotFound {
			t.Fatalf("expected twirp.NotFound error, got %v", err)
		}
	}))
}

func TestServer_ListConversations(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(model.New(ConnectMongo()), nil)

	t.Run("lists conversations newest first, with messages omitted", WithFixture(func(t *testing.T, f *Fixture) {
		older := f.CreateConversation(func(c *model.Conversation) {
			c.CreatedAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		})
		newer := f.CreateConversation(func(c *model.Conversation) {
			c.CreatedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		})

		out, err := srv.ListConversations(ctx, &pb.ListConversationsRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		indexOf := func(id string) int {
			for i, c := range out.GetConversations() {
				if c.GetId() == id {
					return i
				}
			}
			return -1
		}

		olderIdx, newerIdx := indexOf(older.ID.Hex()), indexOf(newer.ID.Hex())
		if olderIdx == -1 || newerIdx == -1 {
			t.Fatalf("expected both conversations to be listed, got older=%d newer=%d", olderIdx, newerIdx)
		}

		if newerIdx >= olderIdx {
			t.Errorf("expected newer conversation (index %d) to come before older one (index %d)", newerIdx, olderIdx)
		}

		for _, id := range []string{older.ID.Hex(), newer.ID.Hex()} {
			c := out.GetConversations()[indexOf(id)]
			if len(c.GetMessages()) != 0 {
				t.Errorf("conversation %s: expected messages to be omitted, got %d", id, len(c.GetMessages()))
			}
		}
	}))
}
