package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"tickets/internal/booking"
)

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var requestKey = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

func main() {
	if err := run(); err != nil {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ttl := 15 * time.Minute
	if value := os.Getenv("BOOKING_TTL"); value != "" {
		var err error
		ttl, err = time.ParseDuration(value)
		if err != nil || ttl < time.Second || ttl > 24*time.Hour {
			return errors.New("BOOKING_TTL must be between 1s and 24h")
		}
	}
	db, err := booking.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	migration, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = booking.Migrate(migration, db, os.Getenv("SEED_DEMO") == "true")
	cancel()
	if err != nil {
		return err
	}

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	server := &http.Server{
		Addr: addr, Handler: routes(db, ttl), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	slog.Info("api listening", "addr", addr)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

func routes(db *sql.DB, ttl time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			jsonResponse(w, 503, map[string]string{"error": "база недоступна"})
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		events, err := booking.Events(r.Context(), db)
		result(w, 200, events, err)
	})
	mux.HandleFunc("POST /api/bookings", func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if !requestKey.MatchString(key) {
			result(w, 400, nil, errors.New("укажите ключ идемпотентности"))
			return
		}
		var input struct {
			EventID  string `json:"event_id"`
			Quantity int    `json:"quantity"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || decoder.Decode(new(any)) != io.EOF ||
			strings.TrimSpace(input.EventID) == "" || len(input.EventID) > 128 || input.Quantity < 1 || input.Quantity > 8 {
			result(w, 400, nil, errors.New("укажите мероприятие и количество билетов от 1 до 8"))
			return
		}
		reservation, err := booking.Create(r.Context(), db, key, input.EventID, input.Quantity, ttl)
		result(w, 201, reservation, err)
	})
	for method, action := range map[string]func(context.Context, *sql.DB, string) (booking.Reservation, error){
		"GET": booking.Get, "DELETE": booking.Cancel,
	} {
		mux.HandleFunc(method+" /api/bookings/{id}", func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if !uuid.MatchString(id) {
				result(w, 400, nil, errors.New("некорректный номер брони"))
				return
			}
			reservation, err := action(r.Context(), db, id)
			result(w, 200, reservation, err)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func result(w http.ResponseWriter, status int, data any, err error) {
	if err == nil {
		jsonResponse(w, status, data)
		return
	}
	message := err.Error()
	switch {
	case status == 400:
	case errors.Is(err, sql.ErrNoRows):
		status, message = 404, "запись не найдена"
	case errors.Is(err, booking.ErrUnavailable), errors.Is(err, booking.ErrKeyConflict):
		status = 409
	default:
		slog.Error("request failed", "error", err)
		status, message = 500, "ошибка сервера, попробуйте снова"
	}
	jsonResponse(w, status, map[string]string{"error": message})
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
