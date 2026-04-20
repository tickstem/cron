// tsk-local is a local development server that mimics the Tickstem API.
//
// It accepts job registrations from the SDK, fires them on their cron
// schedule against your local handler, and shows a dashboard at
// http://localhost:8090.
//
// Install:
//
//	go install github.com/tickstem/cron/cmd/tsk-local@latest
//
// Run:
//
//	tsk-local
//	tsk-local --port 9000
//
// Point your SDK at it:
//
//	client := cron.New("any-key", cron.WithBaseURL("http://localhost:8090/v1"))
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	port := flag.Int("port", 8090, "port to listen on")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	store := newStore()
	sched := newScheduler(nil) // runJob wired below

	srv := newServer(store, sched, log)
	sched.runJob = srv.runJob // wire back-reference

	sched.start()
	defer sched.stop()

	addr := fmt.Sprintf(":%d", *port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("listen failed", "error", err)
		os.Exit(1)
	}

	log.Info("tsk-local started",
		"addr", fmt.Sprintf("http://localhost:%d", *port),
		"dashboard", fmt.Sprintf("http://localhost:%d", *port),
		"api", fmt.Sprintf("http://localhost:%d/v1", *port),
	)
	fmt.Printf("\n  Dashboard  →  http://localhost:%d\n", *port)
	fmt.Printf("  API        →  http://localhost:%d/v1\n\n", *port)
	fmt.Printf("  SDK usage:\n")
	fmt.Printf("    client := cron.New(\"any-key\",\n")
	fmt.Printf("        cron.WithBaseURL(\"http://localhost:%d/v1\"))\n\n", *port)

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-stopCh
	log.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpServer.Shutdown(ctx) //nolint:errcheck
}
