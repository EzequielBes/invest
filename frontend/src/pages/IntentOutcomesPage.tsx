import { api, type IntentOutcome } from '../api/client';
import { usePolling } from '../hooks/usePolling';

export default function IntentOutcomesPage() {
  const { data, error } = usePolling<IntentOutcome[]>(api.intentOutcomes);
  if (error) return <p className="error" role="alert">Erro ao carregar resultados: {error}</p>;
  if (!data) return <p aria-live="polite">Carregando...</p>;
  if (data.length === 0) return <p>Nenhum resultado de intencao disponivel.</p>;
  const correct = data.filter((outcome) => outcome.correct).length;
  const average = data.reduce((sum, outcome) => sum + outcome.direction_return_pct, 0) / data.length;
  return <section>
    <h2>Resultados de Intencoes</h2>
    <p>{correct}/{data.length} corretas ({(correct / data.length * 100).toFixed(0)}%) &middot; retorno medio {average.toFixed(2)}%</p>
    <div className="ledger">
      {data.map((outcome) => <article className={`ledger-entry side-${outcome.side}`} key={`${outcome.analysis_run_id}-${outcome.intent_id}-${outcome.horizon_hours}`}>
        <div className="ledger-head"><span className="ledger-asset">{outcome.asset} &middot; {outcome.side.toUpperCase()} &middot; {outcome.horizon_hours}h</span><span>{outcome.correct ? 'correta' : 'incorreta'}</span></div>
        <div className="ledger-foot">retorno ajustado {outcome.direction_return_pct.toFixed(2)}%</div>
      </article>)}
    </div>
  </section>;
}
