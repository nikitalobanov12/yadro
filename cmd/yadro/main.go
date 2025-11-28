package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nikita/yadro/internal/engine"
	"github.com/nikita/yadro/internal/gateway"
)

func main() {
	// Parse command line flags
	addr := flag.String("addr", ":8080", "HTTP server address")
	walPath := flag.String("wal", "yadro.wal", "WAL file path")
	flag.Parse()

	log.Println("YADRO - High-Performance Limit Order Book Engine")
	log.Println("================================================")

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize engine
	cfg := &engine.Config{
		WALPath:     *walPath,
		ChannelSize: 10000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}

	// Start engine
	eng.Start(ctx)
	log.Println("engine: started")

	// Create and start gateway
	server := gateway.NewServer(eng, *addr)

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		if err := server.Start(ctx); err != nil {
			log.Printf("gateway: server error: %v", err)
			cancel()
		}
	}()

	// Wait for shutdown signal
	sig := <-sigCh
	log.Printf("received signal: %v", sig)

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	log.Println("shutting down...")

	if err := server.Stop(shutdownCtx); err != nil {
		log.Printf("gateway: shutdown error: %v", err)
	}

	cancel() // Stop engine
	eng.Stop()

	log.Println("goodbye!")
}
