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

	"github.com/erick-ti/flightbound/backend/internal/fxrates"
	"github.com/erick-ti/flightbound/backend/internal/tripcomparison"
)

// ecbDailyURL is the ECB's daily euro foreign exchange reference rates.
const ecbDailyURL = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		os.Exit(2)
	}
	rates, err := ratesSource(cfg.ratesFile, cfg.ratesURL, cfg.ratesURLSet)
	if err != nil {
		slog.Error("loading exchange rates failed", "err", err)
		os.Exit(1)
	}
	if err := run(cfg.addr, rates); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

type config struct {
	addr        string
	ratesURL    string
	ratesURLSet bool
	ratesFile   string
}

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	var cfg config
	fs.StringVar(&cfg.addr, "addr", "127.0.0.1:8080", "TCP address to listen on")
	fs.StringVar(&cfg.ratesURL, "fx-rates-url", ecbDailyURL, "URL of the ECB daily euro reference rates")
	fs.StringVar(&cfg.ratesFile, "fx-rates-file", "", "read exchange rates from this saved ECB daily rates file and never fetch them")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	// The URL flag has a default, so only an explicit -fx-rates-url counts
	// as a choice that conflicts with -fx-rates-file.
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "fx-rates-url" {
			cfg.ratesURLSet = true
		}
	})
	return cfg, nil
}

// ratesSource chooses where exchange rates come from. A rates file is read
// once, at startup, and then the server never fetches rates.
func ratesSource(file, url string, urlSet bool) (fxrates.Source, error) {
	if file == "" {
		return fxrates.NewFetcher(url, &http.Client{}), nil
	}
	if urlSet {
		return nil, errors.New("use -fx-rates-file or -fx-rates-url, not both")
	}
	rates, err := fxrates.LoadFile(file)
	if err != nil {
		return nil, err
	}
	slog.Info("using exchange rates from a file", "path", file, "date", rates.Date)
	return fxrates.Static(rates), nil
}

func newMux(rates fxrates.Source) *http.ServeMux {
	mux := http.NewServeMux()
	// Registered without a method so the handler answers other methods with
	// its own JSON 405 response.
	mux.Handle("/api/trip-comparisons", tripcomparison.Handler(rates))
	return mux
}

// run serves until SIGINT or SIGTERM, then lets in-flight requests finish.
func run(addr string, rates fxrates.Source) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           newMux(rates),
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
