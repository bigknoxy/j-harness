// Command harness runs the j-harness agent execution service.
//
// It loads the file-based agent registry, builds an OpenAI-compatible LLM client
// from the environment, and serves the async HTTP API backed by a job store
// (embedded SQLite by default, Redis optional) and a bounded worker pool (see
// docs/API.md).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/bigknoxy/j-harness/internal/api"
	"github.com/bigknoxy/j-harness/internal/engine"
	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/metrics"
	"github.com/bigknoxy/j-harness/internal/registry"
	"github.com/bigknoxy/j-harness/internal/store"
	redisstore "github.com/bigknoxy/j-harness/internal/store/redis"
	"github.com/bigknoxy/j-harness/internal/tools"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	addr := flag.String("addr", envOr("HARNESS_ADDR", "127.0.0.1:8080"), "HTTP listen address")
	registryPath := flag.String("registry", envOr("HARNESS_REGISTRY", "./agent-registry"), "agent registry root directory")
	dbPath := flag.String("db", envOr("HARNESS_DB", "./data/harness.db"), "SQLite database path")
	storeKind := flag.String("store", envOr("HARNESS_STORE", "sqlite"), "job store backend: sqlite or redis")
	workers := flag.Int("workers", envInt("HARNESS_WORKERS", runtime.NumCPU()), "number of worker goroutines")
	retries := flag.Int("retries", envInt("HARNESS_RETRIES", 3), "max LLM attempts per call (1 disables retries)")
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

	st, err := openStore(*storeKind, *dbPath)
	if err != nil {
		log.Fatalf("open %s store: %v", *storeKind, err)
	}
	defer func() { _ = st.Close() }()

	baseClient, err := llm.NewOpenAI(llm.OpenAIConfig{
		BaseURL: envOr("OPENAI_BASE_URL", "http://127.0.0.1:11434/v1"),
		Model:   envOr("OPENAI_MODEL", ""),
		APIKey:  os.Getenv("OPENAI_API_KEY"), // env-only; never logged
	})
	if err != nil {
		log.Fatalf("init llm client: %v", err)
	}

	met := metrics.New()

	// Retries wrap the client, not the engine: only transport failures and
	// HTTP 429/5xx are retried, 4xx fails fast (see docs/MEMORY.md).
	var client llm.Client = llm.NewRetry(baseClient, llm.Retry{
		MaxAttempts: *retries,
		OnRetry:     func() { met.Inc(metrics.LLMRetries) },
	})
	log.Printf("llm retries: max %d attempt(s)", *retries)

	eng, err := engine.New(reg, client)
	if err != nil {
		log.Fatalf("init engine: %v", err)
	}
	eng.SetMetrics(met)
	if os.Getenv("ENABLE_TOOLS") == "true" {
		toolReg, terr := tools.New("current_time", "word_count", "math_eval")
		if terr != nil {
			log.Fatalf("init tools: %v", terr)
		}
		eng.SetTools(toolReg)
		log.Printf("tools enabled: %v", toolReg.Names())
	}

	pool, err := engine.NewPool(engine.PoolConfig{
		Store:   st,
		Engine:  eng,
		Workers: *workers,
		Metrics: met,
	})
	if err != nil {
		log.Fatalf("init worker pool: %v", err)
	}
	defer pool.Close()
	if _, err := pool.RequeueOrphans(context.Background()); err != nil {
		log.Printf("requeue orphaned jobs: %v", err)
	}

	handler, err := api.New(api.Config{
		Engine:    eng,
		Registry:  reg,
		Pool:      pool,
		Store:     st,
		Version:   version,
		AuthToken: os.Getenv("HARNESS_AUTH_TOKEN"), // env-only; never logged
		Metrics:   met,
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

// openStore selects the job store backend. SQLite (the embedded default) has no
// external dependency; Redis is opt-in for shared or restart-durable state.
func openStore(kind, dbPath string) (store.Store, error) {
	switch kind {
	case "sqlite", "":
		st, err := store.Open(dbPath)
		if err != nil {
			return nil, err
		}
		log.Printf("store sqlite %s: ready", dbPath)
		return st, nil
	case "redis":
		cfg := redisstore.Config{
			Addr:      envOr("HARNESS_REDIS_ADDR", "127.0.0.1:6379"),
			Password:  os.Getenv("HARNESS_REDIS_PASSWORD"), // env-only; never logged
			KeyPrefix: envOr("HARNESS_REDIS_PREFIX", "jh:"),
		}
		if v := envOr("HARNESS_REDIS_DB", ""); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				cfg.DB = n
			}
		}
		st, err := redisstore.Open(cfg)
		if err != nil {
			return nil, err
		}
		log.Printf("store redis %s db %d: ready", cfg.Addr, cfg.DB)
		return st, nil
	default:
		return nil, fmt.Errorf("unknown store %q (want sqlite or redis)", kind)
	}
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}
