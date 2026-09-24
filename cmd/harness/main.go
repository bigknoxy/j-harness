// Command harness runs the j-harness agent execution service.
//
// Phase 0 is a bootstrap placeholder: it serves /healthz so the binary,
// container, and CI pipeline are verifiable end to end. The registry,
// engine, and full API are implemented in later phases (see tasks/roadmap.md).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version})
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
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

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
