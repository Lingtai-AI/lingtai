import type { Network } from './types';
import type { EdgeMode } from './Graph';
import type { Theme } from './theme';
import { Stats } from './Stats';
import { t } from './i18n';

export function BottomBar({ network, edgeMode, showNames, showFilter, lang, theme, onToggle, onToggleNames, onToggleFilter }: {
  network: Network;
  edgeMode: EdgeMode;
  showNames: boolean;
  showFilter: boolean;
  lang: string;
  theme: Theme;
  onToggle: () => void;
  onToggleNames: () => void;
  onToggleFilter: () => void;
}) {
  const pill = (active: boolean, color?: string): React.CSSProperties => ({
    background: active ? (color ? color + '20' : theme.textDim + '18') : 'transparent',
    border: `1px solid ${active ? (color ? color + '50' : theme.textDim + '30') : theme.border}`,
    borderRadius: 4,
    padding: '3px 10px',
    cursor: 'pointer',
    color: active ? (color || theme.textDim) : theme.textDim + '55',
    fontSize: 10,
    letterSpacing: 0.5,
    flexShrink: 0,
    transition: 'all 0.15s',
    fontFamily: "'Georgia', 'Noto Serif SC', serif",
  });

  return (
    <div style={{
      background: theme.barBg,
      borderTop: `1px solid ${theme.border}`,
      padding: '8px 16px',
      display: 'flex',
      alignItems: 'center',
      gap: 10,
      flexShrink: 0,
    }}>
      <Stats stats={network.stats} lang={lang} theme={theme} />

      {/* Edge mode segmented control */}
      <div style={{
        display: 'flex',
        flexShrink: 0,
        borderRadius: 4,
        overflow: 'hidden',
        border: `1px solid ${theme.border}`,
      }}>
        {(['avatar', 'email'] as EdgeMode[]).map(mode => {
          const active = edgeMode === mode;
          const color = mode === 'avatar' ? theme.edgeColors.avatar : theme.edgeColors.mail;
          return (
            <button
              key={mode}
              onClick={active ? undefined : onToggle}
              style={{
                background: active ? color + '25' : 'transparent',
                border: 'none',
                borderRight: mode === 'avatar' ? `1px solid ${theme.border}` : 'none',
                padding: '3px 10px',
                cursor: active ? 'default' : 'pointer',
                color: active ? color : color + '55',
                fontSize: 10,
                letterSpacing: 0.5,
                transition: 'all 0.15s',
              }}
            >
              {t(lang, `edge.${mode}`)}
            </button>
          );
        })}
      </div>

      <button onClick={onToggleNames} style={pill(showNames)}>
        {showNames ? 'name ✓' : 'name'}
      </button>

      <button onClick={onToggleFilter} style={pill(showFilter, theme.gold)}>
        filter{showFilter ? ' ✓' : ''}
      </button>
    </div>
  );
}
