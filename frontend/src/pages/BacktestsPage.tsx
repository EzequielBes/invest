import { useState } from 'react';
import { api, type BacktestDetail, type BacktestRun } from '../api/client';
import EquityCurveChart from '../components/EquityCurveChart';
import { usePolling } from '../hooks/usePolling';

export default function BacktestsPage() {
  const { data, error } = usePolling<BacktestRun[]>(api.backtests);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [detail, setDetail] = useState<BacktestDetail | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);

  async function selectRun(id: string) {
    setSelectedID(id); setDetail(null); setDetailError(null);
    try { setDetail(await api.backtestDetail(id)); }
    catch (err) { setDetailError(err instanceof Error ? err.message : String(err)); }
  }

  if (error) return <p className="error" role="alert">Erro ao carregar backtests: {error}</p>;
  if (!data) return <p aria-live="polite">Carregando...</p>;
  if (data.length === 0) return <p>Nenhum backtest encontrado.</p>;

  return <div className="split-view">
    <table className="data-table"><thead><tr><th>Estrategia</th><th>Status</th><th>Retorno</th><th>Sharpe</th><th>Inicio</th></tr></thead>
      <tbody>{data.map((run) => <tr className={run.id === selectedID ? 'selected' : ''} key={run.id}>
        <td><button aria-pressed={run.id === selectedID} className="row-select" onClick={() => void selectRun(run.id)} type="button">{run.strategy_name}</button></td>
        <td>{run.status}</td><td>{run.results ? `${run.results.total_return_pct.toFixed(2)}%` : '-'}</td><td>{run.results ? run.results.sharpe_ratio.toFixed(2) : '-'}</td><td>{new Date(run.started_at).toLocaleString()}</td>
      </tr>)}</tbody>
    </table>
    <section aria-live="polite" className="detail-panel" aria-label="Detalhes do backtest">
      {detailError && <p className="error" role="alert">{detailError}</p>}
      {detail && <><h3>{detail.run.strategy_name} - {detail.run.id}</h3><EquityCurveChart points={detail.equity_curve} />
        <table className="data-table"><thead><tr><th>Data</th><th>Ativo</th><th>Lado</th><th>Qtd</th><th>Preco</th><th>Permitido</th></tr></thead>
          <tbody>{detail.trades.map((trade) => <tr key={`${trade.ts}-${trade.asset}-${trade.side}`}><td>{new Date(trade.ts).toLocaleString()}</td><td>{trade.asset}</td><td>{trade.side}</td><td>{trade.quantity}</td><td>{trade.price}</td><td>{trade.allowed ? 'sim' : `nao: ${trade.reject_reason ?? ''}`}</td></tr>)}</tbody>
        </table>
      </>}
    </section>
  </div>;
}
