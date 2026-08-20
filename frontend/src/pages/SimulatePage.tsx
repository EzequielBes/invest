import { useState } from 'react';
import { api, type TriggerBacktestResponse } from '../api/client';

function defaultDate(daysAgo: number): string {
  const d = new Date();
  d.setDate(d.getDate() - daysAgo);
  return d.toISOString().slice(0, 10);
}

export default function SimulatePage() {
  const [periodStart, setPeriodStart] = useState(defaultDate(30));
  const [periodEnd, setPeriodEnd] = useState(defaultDate(0));
  const [timeframes, setTimeframes] = useState('1h');
  const [drivingTimeframe, setDrivingTimeframe] = useState('1h');
  const [assets, setAssets] = useState('BTC');
  const [initialCash, setInitialCash] = useState('10000');
  const [feePct, setFeePct] = useState('0.001');
  const [maShort, setMaShort] = useState('10');
  const [maLong, setMaLong] = useState('30');

  const [submitting, setSubmitting] = useState(false);
  const [result, setResult] = useState<TriggerBacktestResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError(null);
    setResult(null);
    try {
      const response = await api.triggerBacktest({
        period_start: new Date(`${periodStart}T00:00:00Z`).toISOString(),
        period_end: new Date(`${periodEnd}T00:00:00Z`).toISOString(),
        timeframes: timeframes.split(',').map((t) => t.trim()).filter(Boolean),
        driving_timeframe: drivingTimeframe.trim(),
        assets: assets.split(',').map((a) => a.trim()).filter(Boolean),
        initial_cash: Number(initialCash) || undefined,
        fee_pct: Number(feePct) || undefined,
        ma_short_period: Number(maShort) || undefined,
        ma_long_period: Number(maLong) || undefined,
      });
      setResult(response);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div>
      <p style={{ maxWidth: 620, color: 'var(--paper-dim)', marginTop: 0 }}>
        Roda uma simulacao (media movel) contra dados historicos ja coletados. Nao mexe em dinheiro real
        — o resultado aparece aqui e tambem na aba Backtests.
      </p>
      <form className="form-grid" onSubmit={(event) => void handleSubmit(event)}>
        <div className="field">
          <label htmlFor="period-start">Inicio do periodo</label>
          <input id="period-start" onChange={(e) => setPeriodStart(e.target.value)} required type="date" value={periodStart} />
        </div>
        <div className="field">
          <label htmlFor="period-end">Fim do periodo</label>
          <input id="period-end" onChange={(e) => setPeriodEnd(e.target.value)} required type="date" value={periodEnd} />
        </div>
        <div className="field">
          <label htmlFor="assets">Ativos (separados por virgula)</label>
          <input id="assets" onChange={(e) => setAssets(e.target.value)} required type="text" value={assets} />
        </div>
        <div className="field">
          <label htmlFor="timeframes">Timeframes (separados por virgula)</label>
          <input id="timeframes" onChange={(e) => setTimeframes(e.target.value)} required type="text" value={timeframes} />
        </div>
        <div className="field">
          <label htmlFor="driving-timeframe">Timeframe principal</label>
          <input id="driving-timeframe" onChange={(e) => setDrivingTimeframe(e.target.value)} required type="text" value={drivingTimeframe} />
        </div>
        <div className="field">
          <label htmlFor="initial-cash">Saldo inicial de simulacao</label>
          <input id="initial-cash" min="0" onChange={(e) => setInitialCash(e.target.value)} type="number" value={initialCash} />
        </div>
        <div className="field">
          <label htmlFor="fee-pct">Taxa (fracao, ex 0.001)</label>
          <input id="fee-pct" min="0" onChange={(e) => setFeePct(e.target.value)} step="0.0001" type="number" value={feePct} />
        </div>
        <div className="field">
          <label htmlFor="ma-short">Media curta (candles)</label>
          <input id="ma-short" min="1" onChange={(e) => setMaShort(e.target.value)} type="number" value={maShort} />
        </div>
        <div className="field">
          <label htmlFor="ma-long">Media longa (candles)</label>
          <input id="ma-long" min="1" onChange={(e) => setMaLong(e.target.value)} type="number" value={maLong} />
        </div>
      </form>
      <button className="btn-primary" disabled={submitting} onClick={(event) => void handleSubmit(event)} type="submit">
        {submitting ? 'Rodando...' : 'Rodar simulacao'}
      </button>

      {error && <p className="error" role="alert" style={{ marginTop: 16 }}>{error}</p>}

      {result && (
        <div className="form-result">
          <div className="panel-label">simulacao concluida &middot; {result.backtest_run_id}</div>
          <div className="stat-row" style={{ marginTop: 12, marginBottom: 0 }}>
            <div className="stat">
              <div className={`stat-value${result.total_return_pct < 0 ? ' negative' : ''}`}>{result.total_return_pct.toFixed(2)}%</div>
              <div className="stat-label">retorno total</div>
            </div>
            <div className="stat">
              <div className="stat-value">{result.sharpe_ratio.toFixed(2)}</div>
              <div className="stat-label">sharpe</div>
            </div>
            <div className="stat">
              <div className="stat-value">{result.total_trades}</div>
              <div className="stat-label">trades fechados</div>
            </div>
            <div className="stat">
              <div className="stat-value">{result.trade_attempts}</div>
              <div className="stat-label">tentativas</div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
