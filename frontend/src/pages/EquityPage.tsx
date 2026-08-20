import { api, type EquityPoint } from '../api/client';
import EquityCurveChart from '../components/EquityCurveChart';
import { usePolling } from '../hooks/usePolling';

export default function EquityPage() {
  const { data, error } = usePolling<EquityPoint[]>(api.equitySnapshots);

  if (error) return <p className="error">Erro ao carregar patrimonio: {error}</p>;
  if (!data) return <p>Carregando...</p>;

  return (
    <div>
      <h2>Patrimonio real ao longo do tempo</h2>
      <EquityCurveChart points={data} />
    </div>
  );
}
