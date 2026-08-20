import { useState } from 'react';
import { api, type AnalysisRun, type AnalysisRunDetail } from '../api/client';
import { usePolling } from '../hooks/usePolling';

export default function AnalysisRunsPage() {
  const { data, error } = usePolling<AnalysisRun[]>(api.analysisRuns);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [detail, setDetail] = useState<AnalysisRunDetail | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);

  async function selectRun(id: string) {
    setSelectedID(id); setDetail(null); setDetailError(null);
    try { setDetail(await api.analysisRunDetail(id)); }
    catch (err) { setDetailError(err instanceof Error ? err.message : String(err)); }
  }

  if (error) return <p className="error" role="alert">Erro ao carregar runs: {error}</p>;
  if (!data) return <p aria-live="polite">Carregando...</p>;
  if (data.length === 0) return <p>Nenhuma run de analise encontrada.</p>;

  return (
    <div className="split-view">
      <table className="data-table">
        <thead><tr><th>ID</th><th>Timeframe</th><th>Status</th><th>Inicio</th></tr></thead>
        <tbody>{data.map((run) => (
          <tr className={run.id === selectedID ? 'selected' : ''} key={run.id}>
            <td><button aria-pressed={run.id === selectedID} className="row-select" onClick={() => void selectRun(run.id)} type="button">{run.id}</button></td>
            <td>{run.timeframe}</td><td>{run.status}</td><td>{new Date(run.started_at).toLocaleString('pt-BR')}</td>
          </tr>
        ))}</tbody>
      </table>
      <section aria-live="polite" aria-label="Detalhes da run de analise" className="detail-panel">
        {detailError && <p className="error" role="alert">{detailError}</p>}
        {detail && (
          <>
            <h3>Dossie &middot; {detail.run.id}</h3>
            {detail.results.length === 0 ? <p>Sem resultados.</p> : detail.results.map((result) => (
              <div className="case-file" key={result.id}>
                <div className="case-file-head">{result.agent_type} &middot; {result.asset}</div>
                <p className="case-file-narrative">{result.narrative}</p>
                {Object.keys(result.indicators).length > 0 && (
                  <details>
                    <summary>indicadores</summary>
                    <pre>{JSON.stringify(result.indicators, null, 2)}</pre>
                  </details>
                )}
              </div>
            ))}
          </>
        )}
      </section>
    </div>
  );
}
