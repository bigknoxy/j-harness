// Command harness runs the j-harness agent execution service.
//
// It loads the file-based agent registry, builds an OpenAI-compatible LLM client
// from the environment, and serves the synchronous HTTP API (see docs/API.md).
// Async job execution arrives in a later phase (see tasks/roadmap.md).
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bigknoxy/j-harness/internal/api"
	"github.com/bigknoxy/j-harness/internal/engine"
	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/registry"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	addr := flag.String("addr", envOr("HARNESS_ADDR", "127.0.0.1:8080"), "HTTP listen address")
	registryPath := flag.String("registry", envOr("HARNESS_REGISTRY", "./agent-registry"), "agent registry root directory")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		log.Printf("j-harness %s", version)
		return
	}

	reg, err := registry.Load(*registryPath)
	if err != nil {
		log.Fatalf("load registry %s: %v", *registryPath, err)
	}
	log.Printf("registry %s: %d blueprint(s), %d pipeline(s)",
		*registryPath, len(reg.BlueprintIDs()), len(reg.PipelineIDs()))

	client, err := llm.NewOpenAI(llm.OpenAIConfig{
		BaseURL: envOr("OPENAI_BASE_URL", "http://127.0.0.1:11434/v1"),
		Model:   envOr("OPENAI_MODEL", ""),
		APIKey:  os.Getenv("OPENAI_API_KEY"), // env-only; never logged
	})
	if err != nil {
		log.Fatalf("init llm client: %v", err)
	}
	eng, err := engine.New(reg, client)
	if err != nil {
		log.Fatalf("init engine: %v", err)
	}

	handler, err := api.New(api.Config{
		Engine:    eng,
		Registry:  reg,
		Version:   version,
		AuthToken: os.Getenv("HARNESS_AUTH_TOKEN"), // env-only; never logged
	})
	if err != nil {
		log.Fatalf("init api: %v", err)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("j-harness %s listening on %s", version, *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
