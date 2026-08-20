import { useEffect, useState } from 'react';
import AnalysisRunsPage from './pages/AnalysisRunsPage';
import BacktestsPage from './pages/BacktestsPage';
import DecisionsPage from './pages/DecisionsPage';
import EquityPage from './pages/EquityPage';
import NewsPage from './pages/NewsPage';
import OverviewPage from './pages/OverviewPage';
import PaperTradingPage from './pages/PaperTradingPage';
import RiskStatePage from './pages/RiskStatePage';
import SimulatePage from './pages/SimulatePage';

type Tab = 'overview' | 'decisions' | 'risk' | 'analysis' | 'backtests' | 'simulate' | 'paper' | 'equity' | 'news';

const TABS: { id: Tab; label: string }[] = [
  { id: 'overview', label: 'Visao Geral' },
  { id: 'decisions', label: 'Decisoes' },
  { id: 'risk', label: 'Risco' },
  { id: 'analysis', label: 'Analises' },
  { id: 'backtests', label: 'Backtests' },
  { id: 'simulate', label: 'Simular' },
  { id: 'paper', label: 'Ao Vivo' },
  { id: 'equity', label: 'Patrimonio' },
  { id: 'news', label: 'Noticias' },
];

function useClock() {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(id);
  }, []);
  return now;
}

export default function App() {
  const [tab, setTab] = useState<Tab>('overview');
  const now = useClock();

  return (
    <div className="app">
      <header className="desk-header">
        <div className="desk-title">
          <span aria-hidden="true" className="live-dot" />
          <strong>Mesa de Operacoes</strong>
          <span className="divider">&middot;</span>
          <span>investment-platform</span>
        </div>
        <time className="desk-clock" dateTime={now.toISOString()}>
          {now.toLocaleTimeString('pt-BR')}
        </time>
      </header>
      <nav aria-label="Dashboard" className="tabs" role="tablist">
        {TABS.map((item) => {
          const selected = item.id === tab;
          return (
            <button
              aria-controls={`${item.id}-panel`}
              aria-selected={selected}
              className={selected ? 'tab active' : 'tab'}
              id={`${item.id}-tab`}
              key={item.id}
              onClick={() => setTab(item.id)}
              role="tab"
              type="button"
            >
              {item.label}
            </button>
          );
        })}
      </nav>
      <main
        aria-labelledby={`${tab}-tab`}
        className="content"
        id={`${tab}-panel`}
        role="tabpanel"
      >
        {tab === 'overview' && <OverviewPage />}
        {tab === 'decisions' && <DecisionsPage />}
        {tab === 'risk' && <RiskStatePage />}
        {tab === 'analysis' && <AnalysisRunsPage />}
        {tab === 'backtests' && <BacktestsPage />}
        {tab === 'simulate' && <SimulatePage />}
        {tab === 'paper' && <PaperTradingPage />}
        {tab === 'equity' && <EquityPage />}
        {tab === 'news' && <NewsPage />}
      </main>
    </div>
  );
}
