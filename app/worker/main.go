package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tickets/internal/booking"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := booking.Open()
	if err != nil {
		slog.Error("worker setup failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		batch, cancel := context.WithTimeout(ctx, 5*time.Second)
		expired, confirmed, err := booking.Process(batch, db)
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.Error("worker batch failed", "error", err)
		}
		if expired+confirmed > 0 {
			slog.Info("worker batch processed", "expired", expired, "simulated_confirmations", confirmed)
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}
