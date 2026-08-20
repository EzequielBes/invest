import { api, type ConfigStatus, type Decision, type EquityPoint, type NewsItem, type RiskStateResponse } from '../api/client';
import { usePolling } from '../hooks/usePolling';

function statusTone(status: string): 'sage' | 'rust' | 'brass' {
  if (status === 'normal') return 'sage';
  if (status === 'kill_switch') return 'rust';
  return 'brass';
}

export default function OverviewPage() {
  const risk = usePolling<RiskStateResponse>(api.riskState);
  const decisions = usePolling<Decision[]>(api.decisions);
  const equity = usePolling<EquityPoint[]>(api.equitySnapshots);
  const news = usePolling<NewsItem[]>(api.news);
  const config = usePolling<ConfigStatus>(api.configStatus);

  const latestEquity = equity.data && equity.data.length > 0 ? equity.data[equity.data.length - 1] : null;
  const executedCount = decisions.data?.filter((d) => d.execution_status === 'filled').length ?? 0;
  const pendingCount = decisions.data?.filter((d) => d.side !== 'hold' && !d.execution_status).length ?? 0;

  return (
    <div>
      <div className="overview-grid">
        <div className="panel">
          <div className="panel-label">estado de risco</div>
          <div className="panel-body">
            {risk.error && <span className="error">erro</span>}
            {risk.data && (
              <span className={`plaque-status tone-${statusTone(risk.data.state.status)}`} style={{ fontSize: 18, transform: 'none', border: 'none', padding: 0 }}>
                {risk.data.state.status}
              </span>
            )}
            {!risk.data && !risk.error && <span>carregando...</span>}
          </div>
        </div>

        <div className="panel">
          <div className="panel-label">patrimonio atual</div>
          <div className="panel-body">
            {equity.error && <span className="error">erro</span>}
            {latestEquity && <div className="stat-value">{latestEquity.total_equity.toFixed(2)}</div>}
            {!latestEquity && !equity.error && <span>sem snapshots ainda</span>}
          </div>
        </div>

        <div className="panel">
          <div className="panel-label">decisoes executadas / pendentes</div>
          <div className="panel-body">
            {decisions.error && <span className="error">erro</span>}
            {decisions.data && <div className="stat-value">{executedCount} / {pendingCount}</div>}
          </div>
        </div>

        <div className="panel">
          <div className="panel-label">conexoes</div>
          <div className="panel-body">
            {config.error && <span className="error">erro</span>}
            {config.data && (
              <ul className="config-list">
                <li><span className={`config-dot ${config.data.binance_configured ? 'on' : 'off'}`} />Binance</li>
                <li><span className={`config-dot ${config.data.anthropic_configured ? 'on' : 'off'}`} />Anthropic</li>
                <li><span className={`config-dot ${config.data.openai_configured ? 'on' : 'off'}`} />OpenAI</li>
              </ul>
            )}
          </div>
        </div>
      </div>

      <div className="detail-panel">
        <h3>Ultimas noticias</h3>
        {news.error && <p className="error" role="alert">{news.error}</p>}
        {news.data && news.data.length === 0 && <p>Nenhuma noticia coletada ainda.</p>}
        {news.data && news.data.length > 0 && (
          <div className="news-list">
            {news.data.slice(0, 5).map((item) => (
              <article className="news-item" key={item.id}>
                <a href={item.url} rel="noreferrer" target="_blank">{item.title}</a>
                <div className="ledger-meta">
                  <span>{item.source}</span>
                  <span>{new Date(item.published_at).toLocaleString('pt-BR')}</span>
                </div>
              </article>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
