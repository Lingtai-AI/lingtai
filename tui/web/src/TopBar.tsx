import { useEffect, useState } from 'react';
import type { Theme } from './theme';
import { t } from './i18n';

function formatTime(date: Date): string {
  const h = String(date.getHours()).padStart(2, '0');
  const m = String(date.getMinutes()).padStart(2, '0');
  const s = String(date.getSeconds()).padStart(2, '0');
  return `${h}:${m}:${s}`;
}

export function TopBar({ lang, theme }: { lang: string; theme: Theme }) {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(id);
  }, []);

  return (
    <div style={{
      background: theme.barBg,
      borderBottom: `1px solid ${theme.border}`,
      padding: '6px 16px',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'space-between',
      flexShrink: 0,
      userSelect: 'none',
    }}>
      {/* Live indicator */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <span style={{
          display: 'inline-block',
          width: 6,
          height: 6,
          borderRadius: '50%',
          background: theme.stateColors['ACTIVE'],
          boxShadow: `0 0 4px ${theme.stateColors['ACTIVE']}`,
        }} />
        <span style={{
          fontSize: 10,
          fontWeight: 600,
          letterSpacing: 1,
          color: theme.stateColors['ACTIVE'],
        }}>
          {t(lang, 'topbar.live')}
        </span>
      </div>

      {/* Clock */}
      <div style={{
        fontFamily: 'monospace',
        fontSize: 12,
        color: theme.textDim,
        letterSpacing: 1,
      }}>
        {formatTime(now)}
      </div>
    </div>
  );
}
