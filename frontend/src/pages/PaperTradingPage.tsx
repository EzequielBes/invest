import { useState } from 'react';
import { api, type AutomationControls, type AutomationControlsPatch, type Decision, type SimulationStatus } from '../api/client';
import DecisionLedger from '../components/DecisionLedger';
import { usePolling } from '../hooks/usePolling';

export default function PaperTradingPage() {
  const { data: status, error: statusError } = usePolling<SimulationStatus>(api.simulationStatus);
  const { data: controls, error: controlsError } = usePolling<AutomationControls>(api.automationControls);
  const { data: decisions, error: decisionsError } = usePolling<Decision[]>(api.paperDecisions);
  const [updatingControls, setUpdatingControls] = useState(false);
  const [toggleError, setToggleError] = useState<string | null>(null);

  async function handleControlsPatch(patch: AutomationControlsPatch) {
    setUpdatingControls(true);
    setToggleError(null);
    try {
      await api.patchAutomationControls(patch);
    } catch (err) {
      setToggleError(err instanceof Error ? err.message : String(err));
    } finally {
      setUpdatingControls(false);
    }
  }

  const positions = status ? Object.entries(status.positions) : [];

  return (
    <div>
      <p style={{ maxWidth: 640, color: 'var(--paper-dim)', marginTop: 0 }}>
        Roda o pipeline real de decisao (analise, LLM do strategist, validacao do risk-engine) sem
        investir dinheiro real. Os interruptores controlam os ciclos de papel e testnet, disparados por um
        agente externo (Claude ou Codex). Aqui fica o estado e o historico, pra acompanhar o quao precisas as
        decisoes reais teriam sido antes de confiar dinheiro de verdade a elas.
      </p>

      <div className="plaque">
        {statusError && <p className="error" role="alert">{statusError}</p>}
        {!status && !statusError && <p aria-live="polite">Carregando...</p>}
        {status && (
          <>
            <span className={`plaque-status tone-${controls?.paper_enabled ? 'sage' : 'rust'}`}>
              {controls?.paper_enabled ? 'papel ativo' : 'papel desativado'}
            </span>
            {controlsError && <p className="error" role="alert" style={{ marginTop: 12 }}>{controlsError}</p>}
            {controls && (
              <div className="form-grid" style={{ marginTop: 20 }}>
                <label className="field control-toggle">
                  <span>automacao em papel</span>
                  <input checked={controls.paper_enabled} disabled={updatingControls} onChange={(event) => void handleControlsPatch({ paper_enabled: event.target.checked })} type="checkbox" />
                </label>
                <label className="field control-toggle">
                  <span>automacao na testnet</span>
                  <input checked={controls.testnet_enabled} disabled={updatingControls} onChange={(event) => void handleControlsPatch({ testnet_enabled: event.target.checked })} type="checkbox" />
                </label>
                <label className="field" htmlFor="active-agent">
                  <span>agente ativo</span>
                  <select disabled={updatingControls} id="active-agent" onChange={(event) => void handleControlsPatch({ active_agent: event.target.value as AutomationControls['active_agent'] })} value={controls.active_agent}>
                    <option value="claude_code">Claude Code</option>
                    <option value="codex">Codex</option>
                  </select>
                </label>
              </div>
            )}
            {!controls && !controlsError && <p aria-live="polite" style={{ marginTop: 12 }}>Carregando controles...</p>}
            <div aria-live="polite" style={{ marginTop: 12 }}>
              {updatingControls && 'Aplicando controles...'}
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
