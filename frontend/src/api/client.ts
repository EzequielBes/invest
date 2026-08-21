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

export interface ValidationRun {
  id: string;
  hypothesis_id: string;
  status: string;
  config_hash: string;
  backtest_run_id?: string;
  git_commit?: string;
  error?: string;
  created_at: string;
  completed_at?: string;
}

export interface ValidationMetric {
  id: string;
  name: string;
  value: number;
  segment: string;
  unit: string;
  evidence: Record<string, unknown>;
}

export interface ValidationFinding {
  id: string;
  severity: string;
  rule: string;
  message: string;
  evidence: Record<string, unknown>;
  created_at: string;
}

export interface ValidationRunDetail {
  run: ValidationRun;
  metrics: ValidationMetric[];
  findings: ValidationFinding[];
}

export interface NewsItem {
  id: number;
  source: string;
  published_at: string;
  title: string;
  body: string;
  url: string;
}

export interface ConfigStatus {
  binance_configured: boolean;
  anthropic_configured: boolean;
  openai_configured: boolean;
}

export interface PaperFill {
  id: string;
  asset: string;
  side: string;
  quantity: number;
  price: number;
  created_at: string;
}

export interface SimulationStatus {
  enabled: boolean;
  cash: number;
  positions: Record<string, number>;
  fills: PaperFill[];
}

export interface TriggerBacktestRequest {
  period_start: string;
  period_end: string;
  timeframes: string[];
  driving_timeframe: string;
  assets: string[];
  initial_cash?: number;
  fee_pct?: number;
  ma_short_period?: number;
  ma_long_period?: number;
}

export interface TriggerBacktestResponse {
  backtest_run_id: string;
  trade_attempts: number;
  total_return_pct: number;
  max_drawdown_pct: number;
  sharpe_ratio: number;
  sortino_ratio: number;
  annualized_volatility_pct: number;
  win_rate_pct: number;
  total_trades: number;
  avg_trade_pct: number;
}

async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch(path);
  if (!response.ok) {
    const body: { error?: string } = await response.json().catch(() => ({}));
    throw new Error(body.error ?? `request failed: ${response.status}`);
  }
  return response.json() as Promise<T>;
}

async function postJSON<TReq, TRes>(path: string, payload: TReq): Promise<TRes> {
  const response = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    const body: { error?: string } = await response.json().catch(() => ({}));
    throw new Error(body.error ?? `request failed: ${response.status}`);
  }
  return response.json() as Promise<TRes>;
}

export const api = {
  decisions: () => getJSON<Decision[]>('/api/decisions'),
  riskState: () => getJSON<RiskStateResponse>('/api/risk-state'),
  analysisRuns: () => getJSON<AnalysisRun[]>('/api/analysis-runs'),
  analysisRunDetail: (id: string) => getJSON<AnalysisRunDetail>(`/api/analysis-runs/${id}`),
  backtests: () => getJSON<BacktestRun[]>('/api/backtests'),
  backtestDetail: (id: string) => getJSON<BacktestDetail>(`/api/backtests/${id}`),
  validationRuns: () => getJSON<ValidationRun[]>('/api/validation-runs'),
  validationRunDetail: (id: string) => getJSON<ValidationRunDetail>(`/api/validation-runs/${id}`),
  triggerBacktest: (req: TriggerBacktestRequest) => postJSON<TriggerBacktestRequest, TriggerBacktestResponse>('/api/backtests', req),
  equitySnapshots: () => getJSON<EquityPoint[]>('/api/equity-snapshots'),
  news: () => getJSON<NewsItem[]>('/api/news'),
  configStatus: () => getJSON<ConfigStatus>('/api/config-status'),
  paperDecisions: () => getJSON<Decision[]>('/api/paper-decisions'),
  simulationStatus: () => getJSON<SimulationStatus>('/api/simulation/status'),
  toggleSimulation: (enabled: boolean) => postJSON<{ enabled: boolean }, SimulationStatus>('/api/simulation/toggle', { enabled }),
};
