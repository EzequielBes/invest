import { api, type EquityPoint } from '../api/client';
import EquityCurveChart from '../components/EquityCurveChart';
import { usePolling } from '../hooks/usePolling';

export default function EquityPage() {
  const { data, error } = usePolling<EquityPoint[]>(api.equitySnapshots);

  if (error) return <p className="error" role="alert">Erro ao carregar patrimonio: {error}</p>;
  if (!data) return <p aria-live="polite">Carregando...</p>;
  if (data.length === 0) return <p>Nenhum snapshot de patrimonio encontrado ainda.</p>;

  const latest = data[data.length - 1];

  return (
    <div className="equity-hero">
      <h2>Patrimonio real ao longo do tempo</h2>
      <div className="stat-row">
        <div className="stat">
          <div className="stat-value">{latest.total_equity.toFixed(2)}</div>
          <div className="stat-label">patrimonio total</div>
        </div>
        <div className="stat">
          <div className="stat-value">{latest.cash.toFixed(2)}</div>
          <div className="stat-label">caixa</div>
        </div>
        <div className="stat">
          <div className="stat-value">{latest.positions_value.toFixed(2)}</div>
          <div className="stat-label">posicoes</div>
        </div>
      </div>
      <EquityCurveChart points={data} />
    </div>
  );
}
