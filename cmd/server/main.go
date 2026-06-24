package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/acai-travel/tech-challenge/internal/chat"
	"github.com/acai-travel/tech-challenge/internal/chat/assistant"
	"github.com/acai-travel/tech-challenge/internal/chat/model"
	"github.com/acai-travel/tech-challenge/internal/chat/tools"
	"github.com/acai-travel/tech-challenge/internal/httpx"
	"github.com/acai-travel/tech-challenge/internal/mongox"
	"github.com/acai-travel/tech-challenge/internal/otelx"
	"github.com/acai-travel/tech-challenge/internal/pb"
	"github.com/gorilla/mux"
	"github.com/twitchtv/twirp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracing := otelx.MustSetupTracing("acai-chat-server")
	metricsHandler := otelx.MustSetupMetrics()

	mongo := mongox.MustConnect()

	repo := model.New(mongo)

	loadCtx, loadCancel := context.WithTimeout(ctx, 10*time.Second)
	registry := tools.Default()
	tools.Load(loadCtx, registry)
	loadCancel()

	assist := assistant.New(registry)

	server := chat.NewServer(repo, assist)

	// Configure handler
	handler := mux.NewRouter()
	handler.Use(
		httpx.Logger(),
		httpx.Recovery(),
		httpx.Metrics(),
		httpx.Tracing(),
	)

	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "Hi, my name is Clippy!")
	})

	handler.Handle("/metrics", metricsHandler)

	handler.PathPrefix("/twirp/").Handler(pb.NewChatServiceServer(server, twirp.WithServerJSONSkipDefaults(true)))

	apiServer := &http.Server{Addr: ":8080", Handler: handler}
	debugServer := &http.Server{Addr: "127.0.0.1:6060", Handler: nil}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		go func() {
			<-ctx.Done()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer shutdownCancel()
			_ = apiServer.Shutdown(shutdownCtx)
		}()

		slog.Info("Starting the server...")
		if err := apiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	go func() {
		defer wg.Done()

		go func() {
			<-ctx.Done()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer shutdownCancel()
			_ = debugServer.Shutdown(shutdownCtx)
		}()

		slog.Info("Starting internal debug server...")
		if err := debugServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutting down...")

	wg.Wait()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer shutdownCancel()
	_ = shutdownTracing(shutdownCtx)

	slog.Info("Shutdown complete")
}
