package booking

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	ErrUnavailable = errors.New("недостаточно мест или событие уже началось")
	ErrKeyConflict = errors.New("этот ключ уже использован с другими параметрами")
)

type Event struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Category    string    `json:"category"`
	Venue       string    `json:"venue"`
	Description string    `json:"description"`
	StartsAt    time.Time `json:"starts_at"`
	Remaining   int       `json:"remaining"`
}

type Reservation struct {
	ID             string     `json:"id"`
	EventID        string     `json:"event_id"`
	Quantity       int        `json:"quantity"`
	Status         string     `json:"status"`
	ExpiresAt      time.Time  `json:"expires_at"`
	ConfirmationAt *time.Time `json:"confirmation_at"`
}

func Open() (*sql.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB, seed bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(741231)"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY)"); err != nil {
		return err
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, file := range files {
		name := file.Name()
		if !strings.HasSuffix(name, ".sql") || name == "seed.sql" {
			continue
		}
		var applied bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name=$1)", name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := runSQL(ctx, tx, name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations VALUES ($1)", name); err != nil {
			return err
		}
	}
	if seed {
		if err := runSQL(ctx, tx, "seed.sql"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func runSQL(ctx context.Context, tx *sql.Tx, name string) error {
	source, err := migrations.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(source)); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func Events(ctx context.Context, db *sql.DB) ([]Event, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,title,category,venue,description,starts_at,remaining
		FROM events WHERE starts_at > now() ORDER BY starts_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Title, &event.Category, &event.Venue, &event.Description, &event.StartsAt, &event.Remaining); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

const reservationColumns = "id,event_id,quantity,status,expires_at,confirmation_at"

func readReservation(row *sql.Row) (Reservation, error) {
	var r Reservation
	err := row.Scan(&r.ID, &r.EventID, &r.Quantity, &r.Status, &r.ExpiresAt, &r.ConfirmationAt)
	return r, err
}

func Get(ctx context.Context, db *sql.DB, id string) (Reservation, error) {
	return readReservation(db.QueryRowContext(ctx, "SELECT "+reservationColumns+" FROM reservations WHERE id=$1", id))
}

func Create(ctx context.Context, db *sql.DB, key, eventID string, quantity int, ttl time.Duration) (Reservation, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Reservation{}, err
	}
	defer tx.Rollback()
	// Serialize requests with the same key before checking for a previous result.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 1))", key); err != nil {
		return Reservation{}, err
	}
	r, err := readReservation(tx.QueryRowContext(ctx, "SELECT "+reservationColumns+" FROM reservations WHERE request_key=$1", key))
	switch {
	case err == nil:
		if r.EventID != eventID || r.Quantity != quantity {
			return Reservation{}, ErrKeyConflict
		}
		// Reconstruct the original creation response even after a status transition.
		r.Status = "active"
		r.ConfirmationAt = nil
		return r, nil
	case !errors.Is(err, sql.ErrNoRows):
		return Reservation{}, err
	}
	var capacity int
	err = tx.QueryRowContext(ctx, `UPDATE events SET remaining=remaining-$2
		WHERE id=$1 AND remaining >= $2 AND starts_at > clock_timestamp() RETURNING remaining`, eventID, quantity).Scan(&capacity)
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM events WHERE id=$1)", eventID).Scan(&exists); err != nil {
			return Reservation{}, err
		}
		if !exists {
			return Reservation{}, sql.ErrNoRows
		}
		return Reservation{}, ErrUnavailable
	}
	if err != nil {
		return Reservation{}, err
	}
	r, err = readReservation(tx.QueryRowContext(ctx, `INSERT INTO reservations (request_key,event_id,quantity,expires_at)
		VALUES ($1,$2,$3,LEAST(clock_timestamp()+$4::bigint * interval '1 millisecond',
		(SELECT starts_at FROM events WHERE id=$2))) RETURNING `+reservationColumns, key, eventID, quantity, ttl.Milliseconds()))
	if err != nil {
		return Reservation{}, err
	}
	return r, tx.Commit()
}

func Cancel(ctx context.Context, db *sql.DB, id string) (Reservation, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Reservation{}, err
	}
	defer tx.Rollback()
	r, err := readReservation(tx.QueryRowContext(ctx, `UPDATE reservations SET status=
		CASE WHEN expires_at <= clock_timestamp() THEN 'expired' ELSE 'cancelled' END
		WHERE id=$1 AND status='active' RETURNING `+reservationColumns, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Get(ctx, db, id)
	}
	if err != nil {
		return Reservation{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE events SET remaining=remaining+$2 WHERE id=$1", r.EventID, r.Quantity); err != nil {
		return Reservation{}, err
	}
	return r, tx.Commit()
}

func Process(ctx context.Context, db *sql.DB) (int64, int64, error) {
	// Single reservation per transaction keeps lock ordering consistent with cancellation.
	var expired int64
	for expired < 100 {
		result, err := db.ExecContext(ctx, `WITH candidate AS (
			SELECT id FROM reservations WHERE status='active' AND expires_at <= now()
			ORDER BY expires_at LIMIT 1 FOR UPDATE SKIP LOCKED
		), changed AS (
			UPDATE reservations r SET status='expired' FROM candidate c WHERE r.id=c.id RETURNING event_id,quantity
		) UPDATE events e SET remaining=e.remaining+c.quantity FROM changed c WHERE e.id=c.event_id`)
		if err != nil {
			return expired, 0, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return expired, 0, err
		}
		if n == 0 {
			break
		}
		expired += n
	}
	result, err := db.ExecContext(ctx, `WITH candidates AS (
		SELECT id FROM reservations WHERE status='active' AND confirmation_at IS NULL AND expires_at > now()
		ORDER BY expires_at LIMIT 100 FOR UPDATE SKIP LOCKED
	) UPDATE reservations r SET confirmation_at=clock_timestamp() FROM candidates c WHERE r.id=c.id`)
	if err != nil {
		return expired, 0, err
	}
	confirmed, err := result.RowsAffected()
	return expired, confirmed, err
}
