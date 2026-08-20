import type { EquityPoint } from '../api/client';

const WIDTH = 600;
const HEIGHT = 200;
const PADDING = 20;

export default function EquityCurveChart({ points }: { points: EquityPoint[] }) {
  if (points.length === 0) return <p>Sem dados de equity.</p>;

  const values = points.map((point) => point.total_equity);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = max - min || 1;
  const path = points.map((point, index) => {
    const x = PADDING + (index / (points.length - 1 || 1)) * (WIDTH - 2 * PADDING);
    const y = HEIGHT - PADDING - ((point.total_equity - min) / range) * (HEIGHT - 2 * PADDING);
    return `${index === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(' ');

  return <svg aria-label={`Curva de equity, minimo ${min.toFixed(2)}, maximo ${max.toFixed(2)}`} className="equity-chart" role="img" viewBox={`0 0 ${WIDTH} ${HEIGHT}`}>
    <path d={path} fill="none" stroke="#60a5fa" strokeWidth="2" />
  </svg>;
}
