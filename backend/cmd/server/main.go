// Command server runs the backend HTTP API.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/erick-ti/flightbound/backend/internal/tripcomparison"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "TCP address to listen on")
	flag.Parse()
	if err := run(*addr); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	// Registered without a method so the handler answers other methods with
	// its own JSON 405 response.
	mux.Handle("/api/trip-comparisons", tripcomparison.Handler())
	return mux
}

// run serves until SIGINT or SIGTERM, then lets in-flight requests finish.
func run(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           newMux(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	slog.Info("listening", "addr", ln.Addr().String())

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
