CREATE TABLE IF NOT EXISTS macro_indicators (
    series_id    TEXT NOT NULL,
    observed_at  DATE NOT NULL,
    value        DOUBLE PRECISION NOT NULL,
    fetched_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (series_id, observed_at)
);
