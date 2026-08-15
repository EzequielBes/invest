-- market-data/migrations/001_init.sql
CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE IF NOT EXISTS assets (
    exchange      TEXT NOT NULL,
    symbol        TEXT NOT NULL,
    tracked_since TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (exchange, symbol)
);

CREATE TABLE IF NOT EXISTS candles (
    exchange  TEXT NOT NULL,
    symbol    TEXT NOT NULL,
    timeframe TEXT NOT NULL,
    ts        TIMESTAMPTZ NOT NULL,
    open      DOUBLE PRECISION NOT NULL,
    high      DOUBLE PRECISION NOT NULL,
    low       DOUBLE PRECISION NOT NULL,
    close     DOUBLE PRECISION NOT NULL,
    volume    DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (exchange, symbol, timeframe, ts)
);
SELECT create_hypertable('candles', 'ts', if_not_exists => TRUE);

CREATE TABLE IF NOT EXISTS funding_rates (
    exchange TEXT NOT NULL,
    symbol   TEXT NOT NULL,
    ts       TIMESTAMPTZ NOT NULL,
    rate     DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (exchange, symbol, ts)
);
SELECT create_hypertable('funding_rates', 'ts', if_not_exists => TRUE);

CREATE TABLE IF NOT EXISTS open_interest (
    exchange TEXT NOT NULL,
    symbol   TEXT NOT NULL,
    ts       TIMESTAMPTZ NOT NULL,
    value    DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (exchange, symbol, ts)
);
SELECT create_hypertable('open_interest', 'ts', if_not_exists => TRUE);

CREATE TABLE IF NOT EXISTS liquidations (
    id       BIGSERIAL NOT NULL,
    exchange TEXT NOT NULL,
    symbol   TEXT NOT NULL,
    ts       TIMESTAMPTZ NOT NULL,
    side     TEXT NOT NULL,
    price    DOUBLE PRECISION NOT NULL,
    quantity DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (id, ts)
);
SELECT create_hypertable('liquidations', 'ts', if_not_exists => TRUE);

CREATE TABLE IF NOT EXISTS news_items (
    id           BIGSERIAL PRIMARY KEY,
    source       TEXT NOT NULL,
    published_at TIMESTAMPTZ NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    title        TEXT NOT NULL,
    body         TEXT NOT NULL,
    url          TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS collector_runs (
    id          BIGSERIAL PRIMARY KEY,
    collector   TEXT NOT NULL,
    symbol      TEXT NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    status      TEXT NOT NULL,
    error       TEXT
);
CREATE INDEX IF NOT EXISTS collector_runs_lookup ON collector_runs (collector, symbol, started_at DESC);
