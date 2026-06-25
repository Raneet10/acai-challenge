package chat

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/acai-travel/tech-challenge/internal/chat/model"
	"github.com/acai-travel/tech-challenge/internal/pb"
	"github.com/twitchtv/twirp"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var _ pb.ChatService = (*Server)(nil)

const instrumentationName = "github.com/acai-travel/tech-challenge/internal/chat"

var tracer = otel.Tracer(instrumentationName)

var (
	operationDuration metric.Float64Histogram
	operationErrors   metric.Int64Counter
)

func init() {
	meter := otel.Meter(instrumentationName)

	var err error

	operationDuration, err = meter.Float64Histogram("chat.operation.duration", metric.WithDescription("Duration of internal chat operations (assistant and repository calls)"), metric.WithUnit("s"))
	if err != nil {
		panic(err)
	}

	operationErrors, err = meter.Int64Counter("chat.operation.errors", metric.WithDescription("Number of internal chat operations that returned an error"))
	if err != nil {
		panic(err)
	}
}

// finishOperation records the duration (and, on failure, the error) of a
// traced operation, then ends its span. Call it right after the operation
// completes, with the span and start time from tracer.Start.
func finishOperation(ctx context.Context, span trace.Span, operation string, start time.Time, err error) {
	attrs := metric.WithAttributes(attribute.String("operation", operation))
	operationDuration.Record(ctx, time.Since(start).Seconds(), attrs)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		operationErrors.Add(ctx, 1, attrs)
	}

	span.End()
}

//go:generate go tool mockgen -destination=mock_assistant_test.go -package=chat github.com/acai-travel/tech-challenge/internal/chat Assistant

type Assistant interface {
	Title(ctx context.Context, conv *model.Conversation) (string, error)
	Reply(ctx context.Context, conv *model.Conversation) (string, error)
}

type Server struct {
	repo   *model.Repository
	assist Assistant
}

func NewServer(repo *model.Repository, assist Assistant) *Server {
	return &Server{repo: repo, assist: assist}
}

func (s *Server) StartConversation(ctx context.Context, req *pb.StartConversationRequest) (*pb.StartConversationResponse, error) {
	conversation := &model.Conversation{
		ID:        primitive.NewObjectID(),
		Title:     "Untitled conversation",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Messages: []*model.Message{{
			ID:        primitive.NewObjectID(),
			Role:      model.RoleUser,
			Content:   req.GetMessage(),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	}

	if strings.TrimSpace(req.GetMessage()) == "" {
		return nil, twirp.RequiredArgumentError("message")
	}

	var (
		title    string
		titleErr error
		reply    string
		replyErr error
	)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		titleCtx, titleSpan := tracer.Start(ctx, "assistant.Title")
		titleStart := time.Now()
		title, titleErr = s.assist.Title(titleCtx, conversation)
		finishOperation(titleCtx, titleSpan, "assistant.Title", titleStart, titleErr)
	}()

	go func() {
		defer wg.Done()

		replyCtx, replySpan := tracer.Start(ctx, "assistant.Reply")
		replyStart := time.Now()
		reply, replyErr = s.assist.Reply(replyCtx, conversation)
		finishOperation(replyCtx, replySpan, "assistant.Reply", replyStart, replyErr)
	}()

	wg.Wait()

	if titleErr != nil {
		slog.ErrorContext(ctx, "Failed to generate conversation title", "error", titleErr)
	} else {
		conversation.Title = title
	}

	if replyErr != nil {
		return nil, replyErr
	}

	conversation.Messages = append(conversation.Messages, &model.Message{
		ID:        primitive.NewObjectID(),
		Role:      model.RoleAssistant,
		Content:   reply,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	createCtx, createSpan := tracer.Start(ctx, "repo.CreateConversation")
	createStart := time.Now()
	err := s.repo.CreateConversation(createCtx, conversation)
	finishOperation(createCtx, createSpan, "repo.CreateConversation", createStart, err)
	if err != nil {
		return nil, err
	}

	return &pb.StartConversationResponse{
		ConversationId: conversation.ID.Hex(),
		Title:          conversation.Title,
		Reply:          reply,
	}, nil
}

func (s *Server) ContinueConversation(ctx context.Context, req *pb.ContinueConversationRequest) (*pb.ContinueConversationResponse, error) {
	if req.GetConversationId() == "" {
		return nil, twirp.RequiredArgumentError("conversation_id")
	}

	if strings.TrimSpace(req.GetMessage()) == "" {
		return nil, twirp.RequiredArgumentError("message")
	}

	describeCtx, describeSpan := tracer.Start(ctx, "repo.DescribeConversation")
	describeStart := time.Now()
	conversation, err := s.repo.DescribeConversation(describeCtx, req.GetConversationId())
	finishOperation(describeCtx, describeSpan, "repo.DescribeConversation", describeStart, err)
	if err != nil {
		return nil, err
	}

	conversation.UpdatedAt = time.Now()
	conversation.Messages = append(conversation.Messages, &model.Message{
		ID:        primitive.NewObjectID(),
		Role:      model.RoleUser,
		Content:   req.GetMessage(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	replyCtx, replySpan := tracer.Start(ctx, "assistant.Reply")
	replyStart := time.Now()
	reply, err := s.assist.Reply(replyCtx, conversation)
	finishOperation(replyCtx, replySpan, "assistant.Reply", replyStart, err)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	conversation.Messages = append(conversation.Messages, &model.Message{
		ID:        primitive.NewObjectID(),
		Role:      model.RoleAssistant,
		Content:   reply,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	updateCtx, updateSpan := tracer.Start(ctx, "repo.UpdateConversation")
	updateStart := time.Now()
	err = s.repo.UpdateConversation(updateCtx, conversation)
	finishOperation(updateCtx, updateSpan, "repo.UpdateConversation", updateStart, err)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &pb.ContinueConversationResponse{Reply: reply}, nil
}

func (s *Server) ListConversations(ctx context.Context, req *pb.ListConversationsRequest) (*pb.ListConversationsResponse, error) {
	conversations, err := s.repo.ListConversations(ctx)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	resp := &pb.ListConversationsResponse{}
	for _, conv := range conversations {
		conv.Messages = nil // Clear messages to avoid sending large data
		resp.Conversations = append(resp.Conversations, conv.Proto())
	}

	return resp, nil
}

func (s *Server) DescribeConversation(ctx context.Context, req *pb.DescribeConversationRequest) (*pb.DescribeConversationResponse, error) {
	if req.GetConversationId() == "" {
		return nil, twirp.RequiredArgumentError("conversation_id")
	}

	conversation, err := s.repo.DescribeConversation(ctx, req.GetConversationId())
	if err != nil {
		return nil, err
	}

	if conversation == nil {
		return nil, twirp.NotFoundError("conversation not found")
	}

	return &pb.DescribeConversationResponse{Conversation: conversation.Proto()}, nil
}
