import type { Network } from './types';
import type { EdgeMode } from './Graph';
import type { Theme } from './theme';
import { Stats } from './Stats';
import { t } from './i18n';

export function BottomBar({ network, edgeMode, showNames, lang, theme, onToggle, onToggleNames }: {
  network: Network;
  edgeMode: EdgeMode;
  showNames: boolean;
  lang: string;
  theme: Theme;
  onToggle: () => void;
  onToggleNames: () => void;
}) {
  return (
    <div style={{
      background: theme.barBg,
      borderTop: `1px solid ${theme.border}`,
      padding: '10px 16px',
      display: 'flex',
      alignItems: 'center',
      gap: 12,
      flexShrink: 0,
    }}>
      <Stats stats={network.stats} lang={lang} theme={theme} />
      <div style={{ display: 'flex', flexShrink: 0, alignSelf: 'center', borderRadius: 4, overflow: 'hidden', border: `1px solid ${theme.border}` }}>
        {(['avatar', 'email'] as EdgeMode[]).map(mode => {
          const active = edgeMode === mode;
          const color = mode === 'avatar' ? theme.edgeColors.avatar : theme.edgeColors.mail;
          return (
            <button
              key={mode}
              onClick={active ? undefined : onToggle}
              style={{
                background: active ? color + '30' : 'transparent',
                border: 'none',
                borderRight: mode === 'avatar' ? `1px solid ${theme.border}` : 'none',
                padding: '3px 10px',
                cursor: active ? 'default' : 'pointer',
                color: active ? color : color + '66',
                fontSize: 10,
                letterSpacing: 0.5,
              }}
            >
              {t(lang, `edge.${mode}`)}
            </button>
          );
        })}
      </div>
      {/* Name toggle */}
      <button
        onClick={onToggleNames}
        style={{
          background: showNames ? theme.textDim + '20' : 'transparent',
          border: `1px solid ${theme.border}`,
          borderRadius: 4,
          padding: '3px 10px',
          cursor: 'pointer',
          color: showNames ? theme.textDim : theme.textDim + '66',
          fontSize: 10,
          letterSpacing: 0.5,
          flexShrink: 0,
        }}
        title={showNames ? 'Hide agent names' : 'Show agent names'}
      >
        {showNames ? 'name ✓' : 'name'}
      </button>
    </div>
  );
}
