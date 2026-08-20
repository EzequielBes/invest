import type { EquityPoint } from '../api/client';

const WIDTH = 640;
const HEIGHT = 220;
const PADDING = 30;

export default function EquityCurveChart({ points }: { points: EquityPoint[] }) {
  if (points.length === 0) return <p>Sem dados de equity.</p>;

  const values = points.map((point) => point.total_equity);
  const min = Math.min(...values);
  const rawMax = Math.max(...values);
  const max = rawMax === min ? min + 1 : rawMax;
  const range = max - min;

  const path = points.map((point, index) => {
    const x = PADDING + (index / (points.length - 1 || 1)) * (WIDTH - 2 * PADDING);
    const y = HEIGHT - PADDING - ((point.total_equity - min) / range) * (HEIGHT - 2 * PADDING);
    return `${index === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(' ');

  const gridLines = [0, 0.5, 1].map((fraction) => HEIGHT - PADDING - fraction * (HEIGHT - 2 * PADDING));

  return (
    <svg
      aria-label={`Curva de equity, minimo ${min.toFixed(2)}, maximo ${max.toFixed(2)}`}
      className="equity-chart"
      role="img"
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
    >
      {gridLines.map((y) => (
        <line key={y} style={{ stroke: 'var(--hairline)' }} strokeDasharray="2 4" x1={PADDING} x2={WIDTH - PADDING} y1={y} y2={y} />
      ))}
      <text style={{ fill: 'var(--paper-dim)', fontFamily: 'var(--font-mono)' }} fontSize="10" textAnchor="end" x={WIDTH - PADDING} y={PADDING - 10}>
        {max.toFixed(2)}
      </text>
      <text style={{ fill: 'var(--paper-dim)', fontFamily: 'var(--font-mono)' }} fontSize="10" textAnchor="end" x={WIDTH - PADDING} y={HEIGHT - PADDING + 18}>
        {min.toFixed(2)}
      </text>
      <path d={path} fill="none" style={{ stroke: 'var(--brass-bright)' }} strokeWidth="2" />
    </svg>
  );
}
