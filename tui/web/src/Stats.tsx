import type { NetworkStats } from './types';
import { inkStateColors, inkEdgeColors } from './theme';
import { t } from './i18n';

const items: { key: keyof Omit<NetworkStats, 'total_mails'>; stateKey: string }[] = [
  { key: 'active', stateKey: 'ACTIVE' },
  { key: 'idle', stateKey: 'IDLE' },
  { key: 'stuck', stateKey: 'STUCK' },
  { key: 'asleep', stateKey: 'ASLEEP' },
  { key: 'suspended', stateKey: 'SUSPENDED' },
];

export function Stats({ stats, lang }: { stats: NetworkStats; lang: string }) {
  return (
    <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexShrink: 0 }}>
      {items.map(({ key, stateKey }) => (
        <div key={key} style={{ textAlign: 'center' }}>
          <div style={{ fontSize: 18, fontWeight: 'bold', color: inkStateColors[stateKey] }}>{stats[key]}</div>
          <div style={{ fontSize: 9, color: inkStateColors[stateKey] }}>{t(lang, `state.${key}`)}</div>
        </div>
      ))}
      <div style={{ width: 1, height: 28, background: 'rgba(255,255,255,0.1)', margin: '0 4px' }} />
      <div style={{ textAlign: 'center' }}>
        <div style={{ fontSize: 18, fontWeight: 'bold', color: inkEdgeColors.mail }}>{stats.total_mails}</div>
        <div style={{ fontSize: 9, color: inkEdgeColors.mail }}>{t(lang, 'mails')}</div>
      </div>
    </div>
  );
}
