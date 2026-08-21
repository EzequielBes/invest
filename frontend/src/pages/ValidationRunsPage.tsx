import { useRef, useState } from 'react';
import { api, type ValidationRun, type ValidationRunDetail } from '../api/client';
import { usePolling } from '../hooks/usePolling';

export default function ValidationRunsPage() {
  const { data, error } = usePolling<ValidationRun[]>(api.validationRuns);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [detail, setDetail] = useState<ValidationRunDetail | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  const selectionVersion = useRef(0);

  async function selectRun(id: string) {
    const version = ++selectionVersion.current;
    setSelectedID(id); setDetail(null); setDetailError(null);
    try {
      const response = await api.validationRunDetail(id);
      if (version === selectionVersion.current) setDetail(response);
    } catch (err) {
      if (version === selectionVersion.current) setDetailError(err instanceof Error ? err.message : String(err));
    }
  }

  if (error) return <p className="error" role="alert">Erro ao carregar relatorios: {error}</p>;
  if (!data) return <p aria-live="polite">Carregando...</p>;
  if (data.length === 0) return <p>Nenhum relatorio de validacao encontrado.</p>;

  return (
    <div className="split-view">
      <table className="data-table">
        <thead><tr><th>Hipotese</th><th>Status</th><th>Hash</th><th>Criado em</th></tr></thead>
        <tbody>{data.map((run) => (
          <tr className={run.id === selectedID ? 'selected' : ''} key={run.id}>
            <td><button aria-pressed={run.id === selectedID} className="row-select" onClick={() => void selectRun(run.id)} type="button">{run.hypothesis_id}</button></td>
            <td>{run.status}</td>
            <td>{run.config_hash.slice(0, 12)}</td>
            <td>{new Date(run.created_at).toLocaleString('pt-BR')}</td>
          </tr>
        ))}</tbody>
      </table>
      <section aria-live="polite" aria-label="Detalhes do relatorio de validacao" className="detail-panel">
        {detailError && <p className="error" role="alert">{detailError}</p>}
        {detail && (
          <>
            <h3>Relatorio &middot; {detail.run.id}</h3>
            <p className="ledger-meta">Status: {detail.run.status} | Hash: {detail.run.config_hash}</p>
            {detail.run.error && <p className="error">{detail.run.error}</p>}
            <h4>Metricas</h4>
            {detail.metrics.length === 0 ? <p>Sem metricas registradas.</p> : (
              <table className="data-table">
                <thead><tr><th>Metrica</th><th>Valor</th><th>Segmento</th><th>Unidade</th></tr></thead>
                <tbody>{detail.metrics.map((metric) => (
                  <tr key={metric.id}>
                    <td>{metric.name}</td><td>{metric.value.toLocaleString('pt-BR')}</td><td>{metric.segment}</td><td>{metric.unit}</td>
                  </tr>
                ))}</tbody>
              </table>
            )}
            <h4>Findings</h4>
            {detail.findings.length === 0 ? <p>Sem findings registrados.</p> : detail.findings.map((finding) => (
              <div className="case-file" key={finding.id}>
                <div className="case-file-head">{finding.severity} &middot; {finding.rule}</div>
                <p className="case-file-narrative">{finding.message}</p>
                {Object.keys(finding.evidence).length > 0 && (
                  <details><summary>evidencia</summary><pre>{JSON.stringify(finding.evidence, null, 2)}</pre></details>
                )}
              </div>
            ))}
          </>
        )}
      </section>
    </div>
  );
}
