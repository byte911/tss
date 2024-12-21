package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Load configuration
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	if err := viper.ReadInConfig(); err != nil {
		logger.Fatal("Failed to read config file", zap.Error(err))
	}

	// Connect to NATS
	opts := []nats.Option{
		nats.Name(viper.GetString("app.name")),
		nats.MaxReconnects(viper.GetInt("nats.max_reconnects")),
		nats.ReconnectWait(viper.GetDuration("nats.reconnect_wait")),
	}

	nc, err := nats.Connect(viper.GetString("nats.urls.0"), opts...)
	if err != nil {
		logger.Fatal("Failed to connect to NATS", zap.Error(err))
	}
	defer nc.Close()

	// Create JetStream context
	js, err := nc.JetStream()
	_ = js
	if err != nil {
		logger.Fatal("Failed to create JetStream context", zap.Error(err))
	}

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
		cancel()
	}()

	// Start your services here
	logger.Info("Server started successfully",
		zap.String("app", viper.GetString("app.name")),
		zap.String("version", viper.GetString("app.version")))

	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = shutdownCtx
	defer shutdownCancel()

	// Perform cleanup
	logger.Info("Server shutting down gracefully")
}
