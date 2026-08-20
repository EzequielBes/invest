import { api, type Decision } from '../api/client';
import { usePolling } from '../hooks/usePolling';

export default function DecisionsPage() {
  const { data, error } = usePolling<Decision[]>(api.decisions);

  if (error) return <p className="error" role="alert">Erro ao carregar decisoes: {error}</p>;
  if (!data) return <p aria-live="polite">Carregando...</p>;
  if (data.length === 0) return <p>Nenhuma decisao encontrada.</p>;

  return (
    <table className="data-table">
      <thead><tr><th>Ativo</th><th>Lado</th><th>Confianca</th><th>Risco</th><th>Execucao</th><th>Criado em</th></tr></thead>
      <tbody>
        {data.map((decision) => (
          <tr key={decision.id}>
            <td>{decision.asset}</td><td>{decision.side}</td><td>{(decision.confidence * 100).toFixed(0)}%</td>
            <td>{decision.risk_allowed === undefined ? '-' : decision.risk_allowed ? 'aprovado' : `rejeitado: ${decision.risk_reasons?.join('; ') ?? ''}`}</td>
            <td>{decision.execution_status ? `${decision.execution_status} (${decision.execution_filled_quantity ?? 0} @ ${decision.execution_filled_price ?? 0})` : '-'}</td>
            <td>{new Date(decision.created_at).toLocaleString()}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
