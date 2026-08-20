import { useState } from 'react';
import AnalysisRunsPage from './pages/AnalysisRunsPage';
import BacktestsPage from './pages/BacktestsPage';
import DecisionsPage from './pages/DecisionsPage';
import EquityPage from './pages/EquityPage';
import RiskStatePage from './pages/RiskStatePage';

type Tab = 'decisions' | 'risk' | 'analysis' | 'backtests' | 'equity';

const TABS: { id: Tab; label: string }[] = [
  { id: 'decisions', label: 'Decisoes' },
  { id: 'risk', label: 'Risco' },
  { id: 'analysis', label: 'Analises' },
  { id: 'backtests', label: 'Backtests' },
  { id: 'equity', label: 'Patrimonio' },
];

export default function App() {
  const [tab, setTab] = useState<Tab>('decisions');

  return (
    <div className="app">
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
        {tab === 'decisions' && <DecisionsPage />}
        {tab === 'risk' && <RiskStatePage />}
        {tab === 'analysis' && <AnalysisRunsPage />}
        {tab === 'backtests' && <BacktestsPage />}
        {tab === 'equity' && <EquityPage />}
      </main>
    </div>
  );
}
