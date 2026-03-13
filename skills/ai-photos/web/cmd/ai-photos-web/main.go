package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ai-photos-web/internal/app"
)

func main() {
	var opts app.Options
	flag.StringVar(&opts.ProfileRef, "profile", "", "album profile name or path")
	flag.StringVar(&opts.Host, "host", "127.0.0.1", "listen host")
	flag.IntVar(&opts.Port, "port", 0, "listen port, 0 picks a free port")
	flag.StringVar(&opts.CacheDir, "cache-dir", "", "cache directory for thumbnails and previews")
	flag.BoolVar(&opts.OpenBrowser, "open-browser", false, "open the local URL in the default browser")
	flag.Parse()

	cfg, err := app.LoadConfig(opts)
	if err != nil {
		log.Fatal(err)
	}

	server, err := app.NewServer(cfg)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.CheckReady(ctx); err != nil {
		log.Fatal(err)
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.Host, cfg.PortString()))
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	url := app.LocalURL(listener.Addr())
	startup := map[string]any{
		"type":         "server-started",
		"host":         cfg.Host,
		"port":         app.PortFromAddr(listener.Addr()),
		"url":          url,
		"backend":      cfg.BackendKind,
		"profile_path": cfg.ProfilePath,
		"source":       cfg.ConfigSource,
		"cache_dir":    cfg.CacheDir,
	}
	_ = json.NewEncoder(os.Stdout).Encode(startup)

	if cfg.OpenBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			_ = app.OpenBrowser(url)
		}()
	}

	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
