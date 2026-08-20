import type { Decision } from '../api/client';
import StampBadge from './StampBadge';

function riskStamp(decision: Decision) {
  if (decision.risk_allowed === undefined) return <StampBadge tone="dim">sem avaliacao</StampBadge>;
  return decision.risk_allowed
    ? <StampBadge tone="sage">aprovado</StampBadge>
    : <StampBadge tone="rust">rejeitado</StampBadge>;
}

function executionStamp(decision: Decision) {
  if (!decision.execution_status) return null;
  const tone = decision.execution_status === 'filled' ? 'sage' : decision.execution_status === 'cancelled' ? 'dim' : 'brass';
  return <StampBadge tone={tone}>{decision.execution_status}</StampBadge>;
}

export default function DecisionLedger({ decisions }: { decisions: Decision[] }) {
  if (decisions.length === 0) return <p>Nenhuma decisao encontrada.</p>;
  return (
    <div className="ledger">
      {decisions.map((decision) => (
        <article className={`ledger-entry side-${decision.side}`} key={decision.id}>
          <div className="ledger-head">
            <span className="ledger-asset">{decision.asset} &middot; {decision.side.toUpperCase()}</span>
            <span className="ledger-meta">
              <span>confianca {(decision.confidence * 100).toFixed(0)}%</span>
              <span>{new Date(decision.created_at).toLocaleString('pt-BR')}</span>
            </span>
          </div>
          {decision.rationale && <p className="ledger-rationale">&ldquo;{decision.rationale}&rdquo;</p>}
          <div className="ledger-foot">
            {riskStamp(decision)}
            {executionStamp(decision)}
            {decision.execution_filled_quantity !== undefined && decision.execution_filled_price !== undefined && (
              <span className="ledger-meta">executado {decision.execution_filled_quantity} @ {decision.execution_filled_price}</span>
            )}
            {decision.risk_allowed === false && decision.risk_reasons && decision.risk_reasons.length > 0 && (
              <span className="ledger-meta">{decision.risk_reasons.join('; ')}</span>
            )}
          </div>
        </article>
      ))}
    </div>
  );
}
