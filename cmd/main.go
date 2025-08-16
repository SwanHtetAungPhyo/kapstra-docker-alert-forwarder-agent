package main

import (
	"awesomeProject/internal/config"
	"awesomeProject/pkg/alertengine"
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	loader := config.NewLoader()
	cfg, err := loader.Load()
	if err != nil {
		log.Fatal("Failed to load configuration:", err)
	}

	engine, err := alertengine.New(cfg)
	if err != nil {
		log.Fatal("Failed to create engine:", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutdown signal received")
		cancel()
	}()

	if err := engine.Start(ctx); err != nil {
		log.Fatal("Engine failed:", err)
	}
}
