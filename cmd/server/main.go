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
	// Using structured JSON logging from the very top ensures that log ingestors
// can immediately index labels (like environment, service name) without regex parsing.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Environment variables dictate configuration adhering to 12-factor app principles,
// preventing secrets from leaking into source code and allowing seamless cross-environment deployments.
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:secret@localhost:5432/contextengine?sslmode=disable")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	ollamaURL := getEnv("OLLAMA_URL", "http://localhost:11434")
	ollamaModel := getEnv("OLLAMA_MODEL", "llama3.1:8b")
	port := getEnv("PORT", "8080")

	// We establish a synchronous connection to Postgres here to fail fast if the primary
// persistence store is unavailable, preventing the app from starting in a broken state.
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

	// Redis functions as both our high-speed cache and event broker. This dual use
// minimizes operational complexity while fulfilling our latency and pub-sub requirements.
	redisCache := cache.NewRedisCache(redisAddr)
	if err := redisCache.Ping(context.Background()); err != nil {
		logger.Error("redis ping failed", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to redis")

	// A global budget manager enforces a hard upper bound of 300ms across the entire workflow.
// This constraint guides the behavior of all downstream components, forcing them to shed load
// or return partial data rather than breaking the latency SLA.
	budget := resilience.NewBudgetManager(300 * time.Millisecond)

	// The context enrichment pipeline uses the Strategy pattern to assemble various data sources.
// This modularity ensures we can drop in new signals (e.g., traffic, weather) without rewriting the core engine.
	enrichers := []contextpkg.Enricher{
		contextpkg.NewWeatherEnricher(),
		contextpkg.NewTimeEnricher(),
		contextpkg.NewLocationEnricher(),
		contextpkg.NewPreferenceEnricher(),
	}
	pipeline := contextpkg.NewPipeline(enrichers, redisCache, budget, logger)

	// The repository abstracts the SQL dialect, enabling isolated unit testing of business logic
// by swapping out the real implementation for an in-memory mock.
	partnerRepo := partner.NewRepository(db)

	// Abstracting LLM specifics allows us to switch from Ollama/Llama to OpenAI or Anthropic
// merely by injecting a different implementation of the llmClient interface.
	llmClient := llm.NewOllamaClient(ollamaURL, ollamaModel)

	// The decision engine serves as the nexus, aggregating rich context with domain policies
// to decide whether an LLM call is appropriate or if rule-based fallback is required.
	decisionEngine := engine.NewDecisionEngine(llmClient, partnerRepo, redisCache, budget, logger)

	// By separating publishing from consumption, we achieve a durable, decoupled async workflow
// for things like analytics and delayed actions, reducing synchronous complexity.
	publisher := events.NewPublisher(redisCache.Client(), logger)
	analytics := events.NewAnalyticsConsumer(redisCache.Client(), logger)

	// The analytics consumer runs in the background. It is tied to a context that is
// deliberately cancelled during shutdown to ensure clean termination without trailing connections.
	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	analytics.Start(consumerCtx)

	// We manually inject dependencies rather than using an auto-wiring framework.
// This explicit DI structure keeps compile times fast and makes the dependency graph obviously traceable.
	handler := api.NewHandler(pipeline, decisionEngine, partnerRepo, redisCache, publisher, analytics, logger)
	router := api.NewRouter(handler, logger, 15*time.Second)

	// Tuning the HTTP server timeouts is a critical resilience measure against Slowloris-style attacks
// and zombie connections holding onto file descriptors.
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown intercepts OS signals to prevent abruptly severing active client requests.
// It guarantees a window where in-flight transactions can commit safely before process death.
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

	// We block the main goroutine here until an interrupt signal is caught.
// Proceeding past this point signifies the beginning of the deliberate teardown phase.
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
