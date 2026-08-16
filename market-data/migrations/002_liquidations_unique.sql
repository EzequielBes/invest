-- market-data/migrations/002_liquidations_unique.sql
-- Dedup existing liquidation rows (OKX's REST-polling StreamLiquidations
-- re-ingested the trailing ~100 fills on every process restart, since its
-- in-memory `seen` dedup map resets on restart) and add a natural-key unique
-- constraint so InsertLiquidations can ON CONFLICT DO NOTHING going forward.
DELETE FROM liquidations a USING liquidations b
WHERE a.id < b.id
  AND a.exchange = b.exchange
  AND a.symbol = b.symbol
  AND a.ts = b.ts
  AND a.side = b.side
  AND a.price = b.price
  AND a.quantity = b.quantity;

ALTER TABLE liquidations ADD CONSTRAINT liquidations_natural_key UNIQUE (exchange, symbol, ts, side, price, quantity);
