import { useState } from 'react';
import { api, type Decision, type SimulationStatus } from '../api/client';
import DecisionLedger from '../components/DecisionLedger';
import { usePolling } from '../hooks/usePolling';

export default function PaperTradingPage() {
  const { data: status, error: statusError } = usePolling<SimulationStatus>(api.simulationStatus);
  const { data: decisions, error: decisionsError } = usePolling<Decision[]>(api.paperDecisions);
  const [toggling, setToggling] = useState(false);
  const [toggleError, setToggleError] = useState<string | null>(null);

  async function handleToggle() {
    if (!status) return;
    setToggling(true);
    setToggleError(null);
    try {
      await api.toggleSimulation(!status.enabled);
    } catch (err) {
      setToggleError(err instanceof Error ? err.message : String(err));
    } finally {
      setToggling(false);
    }
  }

  const positions = status ? Object.entries(status.positions) : [];

  return (
    <div>
      <p style={{ maxWidth: 640, color: 'var(--paper-dim)', marginTop: 0 }}>
        Roda o pipeline real de decisao (analise, LLM do strategist, validacao do risk-engine) sem
        investir dinheiro real nem tocar na conta da testnet — cada ciclo e disparado via MCP
        (Claude ou Codex). Aqui fica o interruptor e o historico, pra acompanhar o quao precisas as
        decisoes reais teriam sido antes de confiar dinheiro de verdade a elas.
      </p>

      <div className="plaque">
        {statusError && <p className="error" role="alert">{statusError}</p>}
        {!status && !statusError && <p aria-live="polite">Carregando...</p>}
        {status && (
          <>
            <span className={`plaque-status tone-${status.enabled ? 'sage' : 'rust'}`}>
              {status.enabled ? 'simulacao ativa' : 'simulacao desativada'}
            </span>
            <div style={{ marginTop: 18 }}>
              <button className="btn-primary" disabled={toggling} onClick={() => void handleToggle()} type="button">
                {toggling ? 'Aplicando...' : status.enabled ? 'Desativar simulacao' : 'Ativar simulacao'}
              </button>
            </div>
            {toggleError && <p className="error" role="alert" style={{ marginTop: 12 }}>{toggleError}</p>}
            <div className="stat-row" style={{ marginTop: 20 }}>
              <div className="stat">
                <div className="stat-value">{status.cash.toFixed(2)}</div>
                <div className="stat-label">caixa simulado</div>
              </div>
              <div className="stat">
                <div className="stat-value">{positions.length === 0 ? '—' : positions.map(([asset, qty]) => `${asset} ${qty}`).join(', ')}</div>
                <div className="stat-label">posicoes abertas</div>
              </div>
            </div>
          </>
        )}
      </div>

      <h3>Decisoes simuladas</h3>
      {decisionsError && <p className="error" role="alert">{decisionsError}</p>}
      {!decisions && !decisionsError && <p aria-live="polite">Carregando...</p>}
      {decisions && <DecisionLedger decisions={decisions} />}

      {status && status.fills.length > 0 && (
        <>
          <h3 style={{ marginTop: 24 }}>Preenchimentos simulados</h3>
          <table className="data-table">
            <thead>
              <tr><th>ativo</th><th>lado</th><th>quantidade</th><th>preco</th><th>quando</th></tr>
            </thead>
            <tbody>
              {status.fills.map((fill) => (
                <tr key={fill.id}>
                  <td>{fill.asset}</td>
                  <td>{fill.side}</td>
                  <td>{fill.quantity}</td>
                  <td>{fill.price.toFixed(2)}</td>
                  <td>{new Date(fill.created_at).toLocaleString('pt-BR')}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </div>
  );
}
