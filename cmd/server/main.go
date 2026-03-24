package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/aaayushm23/context-engine/internal/api"
	"github.com/aaayushm23/context-engine/internal/cache"
	contextpkg "github.com/aaayushm23/context-engine/internal/context"
	"github.com/aaayushm23/context-engine/internal/engine"
	"github.com/aaayushm23/context-engine/internal/events"
	"github.com/aaayushm23/context-engine/internal/llm"
	"github.com/aaayushm23/context-engine/internal/partner"
	"github.com/aaayushm23/context-engine/internal/resilience"
)

func main() {
	// Structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Config from environment
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:secret@localhost:5432/contextengine?sslmode=disable")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	ollamaURL := getEnv("OLLAMA_URL", "http://localhost:11434")
	ollamaModel := getEnv("OLLAMA_MODEL", "llama3.1:8b")
	port := getEnv("PORT", "8080")

	// Connect to Postgres
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		logger.Error("postgres ping failed", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to postgres")

	// Redis cache
	redisCache := cache.NewRedisCache(redisAddr)
	if err := redisCache.Ping(context.Background()); err != nil {
		logger.Error("redis ping failed", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to redis")

	// Budget manager (total 300ms)
	budget := resilience.NewBudgetManager(300 * time.Millisecond)

	// Context enrichment pipeline
	enrichers := []contextpkg.Enricher{
		contextpkg.NewWeatherEnricher(),
		contextpkg.NewTimeEnricher(),
		contextpkg.NewLocationEnricher(),
		contextpkg.NewPreferenceEnricher(),
	}
	pipeline := contextpkg.NewPipeline(enrichers, redisCache, budget, logger)

	// Partner repository
	partnerRepo := partner.NewRepository(db)

	// LLM client (Ollama)
	llmClient := llm.NewOllamaClient(ollamaURL, ollamaModel)

	// Decision engine
	decisionEngine := engine.NewDecisionEngine(llmClient, partnerRepo, redisCache, budget, logger)

	// Event system
	publisher := events.NewPublisher(redisCache.Client(), logger)
	analytics := events.NewAnalyticsConsumer(redisCache.Client(), logger)

	// Start analytics consumer
	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	analytics.Start(consumerCtx)

	// HTTP handler — cache passed for health checks
	handler := api.NewHandler(pipeline, decisionEngine, partnerRepo, redisCache, publisher, analytics, logger)
	router := api.NewRouter(handler, logger, 15*time.Second)

	// HTTP server
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("server starting", "port", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	logger.Info("context-engine ready",
		"port", port,
		"ollama_model", ollamaModel,
		"timeout_budget_ms", 300,
	)

	// Wait for shutdown signal
	<-done
	logger.Info("shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
	consumerCancel()

	logger.Info("server stopped")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
