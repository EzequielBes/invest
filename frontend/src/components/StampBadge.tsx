import type { ReactNode } from 'react';

type Tone = 'sage' | 'rust' | 'brass' | 'dim';

export default function StampBadge({ tone, children }: { tone: Tone; children: ReactNode }) {
  return <span className={`stamp stamp-${tone}`}>{children}</span>;
}
