import { api, type RiskStateResponse } from '../api/client';
import { usePolling } from '../hooks/usePolling';

const LIMITS: { key: keyof RiskStateResponse['limits']; label: string }[] = [
  { key: 'max_pct_per_asset', label: '% max por ativo' },
  { key: 'max_pct_crypto_total', label: '% max total cripto' },
  { key: 'max_value_per_trade', label: 'Valor max por trade' },
  { key: 'max_daily_loss', label: 'Perda diaria max' },
  { key: 'max_weekly_loss', label: 'Perda semanal max' },
  { key: 'max_drawdown', label: 'Drawdown max' },
  { key: 'max_consecutive_losses', label: 'Perdas consecutivas max' },
  { key: 'max_volatility', label: 'Volatilidade max' },
  { key: 'min_liquidity', label: 'Liquidez min' },
  { key: 'max_data_age_minutes', label: 'Idade max do dado (min)' },
];

export default function RiskStatePage() {
  const { data, error } = usePolling<RiskStateResponse>(api.riskState);

  if (error) return <p className="error" role="alert">Erro ao carregar estado de risco: {error}</p>;
  if (!data) return <p aria-live="polite">Carregando...</p>;

  return <div className="risk-state">
    <h2>Status: {data.state.status}</h2>
    <p>{data.state.reason}</p>
    <p>Atualizado em: {new Date(data.state.changed_at).toLocaleString()}</p>
    <table className="data-table">
      <thead><tr><th>Limite</th><th>Valor configurado</th></tr></thead>
      <tbody>{LIMITS.map(({ key, label }) => <tr key={key}><td>{label}</td><td>{data.limits[key]}</td></tr>)}</tbody>
    </table>
  </div>;
}
