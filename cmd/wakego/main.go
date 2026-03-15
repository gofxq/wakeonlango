package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wakego/internal/applog"
	"wakego/internal/config"
	"wakego/internal/server"
)

func main() {
	var (
		addr       = flag.String("addr", ":8080", "HTTP listen address")
		configPath = flag.String("config", "config.json", "Path to config file")
		logPath    = flag.String("log-file", "logs/wakego.log", "Path to application log file")
	)
	flag.Parse()

	logger, closeLog, err := applog.New(applog.Options{
		FilePath: *logPath,
		Prefix:   "[wakego] ",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	defer closeLog()

	store, err := config.NewStore(*configPath)
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}

	handler, err := server.New(server.Options{
		Store:  store,
		Logger: logger,
	})
	if err != nil {
		logger.Fatalf("build server: %v", err)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Printf("listening on %s with config %s", *addr, *configPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Printf("shutdown: %v", err)
	}
}
