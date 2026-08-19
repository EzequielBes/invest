CREATE TABLE IF NOT EXISTS executions (
    id                 TEXT PRIMARY KEY,
    asset              TEXT NOT NULL,
    side               TEXT NOT NULL,
    requested_quantity DOUBLE PRECISION NOT NULL,
    price              DOUBLE PRECISION NOT NULL,
    order_id           TEXT NOT NULL DEFAULT '',
    client_order_id    TEXT NOT NULL,
    status             TEXT NOT NULL,
    filled_quantity    DOUBLE PRECISION NOT NULL DEFAULT 0,
    filled_price       DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL
);
