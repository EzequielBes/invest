import { api, type Decision } from '../api/client';
import DecisionLedger from '../components/DecisionLedger';
import { usePolling } from '../hooks/usePolling';

export default function DecisionsPage() {
  const { data, error } = usePolling<Decision[]>(api.decisions);

  if (error) return <p className="error" role="alert">Erro ao carregar decisoes: {error}</p>;
  if (!data) return <p aria-live="polite">Carregando...</p>;
  return <DecisionLedger decisions={data} />;
}
