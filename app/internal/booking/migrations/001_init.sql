CREATE TABLE events (
    id text PRIMARY KEY,
    title text NOT NULL,
    category text NOT NULL,
    venue text NOT NULL,
    description text NOT NULL,
    starts_at timestamptz NOT NULL,
    capacity integer NOT NULL CHECK (capacity > 0),
    remaining integer NOT NULL CHECK (remaining BETWEEN 0 AND capacity)
);

CREATE TABLE reservations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_key text NOT NULL UNIQUE,
    event_id text NOT NULL REFERENCES events(id),
    quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 8),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'cancelled', 'expired')),
    expires_at timestamptz NOT NULL,
    confirmation_at timestamptz
);

CREATE INDEX reservations_expiry ON reservations (expires_at) WHERE status = 'active';
CREATE INDEX reservations_confirmation ON reservations (expires_at)
    WHERE status = 'active' AND confirmation_at IS NULL;
