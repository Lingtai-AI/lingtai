import type { Network } from './types';
import type { EdgeMode } from './Graph';
import { Stats } from './Stats';
import { Kanban } from './Kanban';
import { inkBorder, inkEdgeColors } from './theme';
import { t } from './i18n';

export function BottomBar({ network, edgeMode, lang, onToggle }: {
  network: Network;
  edgeMode: EdgeMode;
  lang: string;
  onToggle: () => void;
}) {
  return (
    <div style={{
      background: 'rgba(13,13,15,0.95)',
      borderTop: `1px solid ${inkBorder}`,
      padding: '10px 16px',
      display: 'flex',
      alignItems: 'flex-start',
      gap: 24,
    }}>
      <Stats stats={network.stats} lang={lang} />
      <button
        onClick={onToggle}
        style={{
          background: 'transparent',
          border: `1px solid ${inkEdgeColors.mail}`,
          borderRadius: 4,
          padding: '3px 10px',
          cursor: 'pointer',
          color: inkEdgeColors.mail,
          fontSize: 10,
          letterSpacing: 0.5,
          flexShrink: 0,
          alignSelf: 'center',
        }}
      >
        {edgeMode === 'avatar' ? t(lang, 'edge.avatar') : t(lang, 'edge.email')}
      </button>
      <Kanban nodes={network.nodes} lang={lang} />
    </div>
  );
}
