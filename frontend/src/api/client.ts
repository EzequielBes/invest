export interface Decision {
  id: string;
  analysis_run_id: string;
  asset: string;
  side: string;
  confidence: number;
  sizing_pct: number;
  rationale: string;
  proposed_quantity: number;
  proposed_value: number;
  risk_allowed?: boolean;
  risk_reasons?: string[];
  execution_status?: string;
  execution_order_id?: string;
  execution_filled_quantity?: number;
  execution_filled_price?: number;
  created_at: string;
}

export interface RiskState {
  status: string;
  reason: string;
  changed_at: string;
}

export interface RiskLimits {
  max_pct_per_asset: number;
  max_pct_crypto_total: number;
  max_value_per_trade: number;
  max_daily_loss: number;
  max_weekly_loss: number;
  max_drawdown: number;
  max_consecutive_losses: number;
  max_volatility: number;
  min_liquidity: number;
  max_data_age_minutes: number;
}

export interface RiskStateResponse {
  state: RiskState;
  limits: RiskLimits;
}

export interface AnalysisRun {
  id: string;
  started_at: string;
  finished_at?: string;
  timeframe: string;
  status: string;
  error?: string;
}

export interface AnalysisResult {
  id: string;
  run_id: string;
  agent_type: string;
  asset: string;
  indicators: Record<string, unknown>;
  narrative: string;
  created_at: string;
}

export interface AnalysisRunDetail {
  run: AnalysisRun;
  results: AnalysisResult[];
}

export interface BacktestResults {
  total_return_pct: number;
  max_drawdown_pct: number;
  sharpe_ratio: number;
  sortino_ratio: number;
  annualized_volatility_pct: number;
  win_rate_pct: number;
  total_trades: number;
  avg_trade_pct: number;
}

export interface BacktestRun {
  id: string;
  strategy_name: string;
  period_start: string;
  period_end: string;
  timeframes: string[];
  driving_timeframe: string;
  initial_cash: number;
  fee_pct: number;
  status: string;
  error?: string;
  started_at: string;
  ended_at?: string;
  results?: BacktestResults;
}

export interface BacktestTrade {
  ts: string;
  asset: string;
  side: string;
  quantity: number;
  price: number;
  fee: number;
  allowed: boolean;
  reject_reason?: string;
}

export interface EquityPoint {
  ts: string;
  cash: number;
  positions_value: number;
  total_equity: number;
}

export interface BacktestDetail {
  run: BacktestRun;
  trades: BacktestTrade[];
  equity_curve: EquityPoint[];
}

async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch(path);
  if (!response.ok) {
    const body: { error?: string } = await response.json().catch(() => ({}));
    throw new Error(body.error ?? `request failed: ${response.status}`);
  }
  return response.json() as Promise<T>;
}

export const api = {
  decisions: () => getJSON<Decision[]>('/api/decisions'),
  riskState: () => getJSON<RiskStateResponse>('/api/risk-state'),
  analysisRuns: () => getJSON<AnalysisRun[]>('/api/analysis-runs'),
  analysisRunDetail: (id: string) => getJSON<AnalysisRunDetail>(`/api/analysis-runs/${id}`),
  backtests: () => getJSON<BacktestRun[]>('/api/backtests'),
  backtestDetail: (id: string) => getJSON<BacktestDetail>(`/api/backtests/${id}`),
};
