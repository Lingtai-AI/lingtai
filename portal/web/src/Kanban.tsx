import type { AgentNode } from './types';
import type { Theme } from './theme';
import { t } from './i18n';

const states = ['ACTIVE', 'IDLE', 'STUCK', 'ASLEEP', 'SUSPENDED', ''];

export function Kanban({ nodes, lang, theme }: { nodes: AgentNode[]; lang: string; theme: Theme }) {
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 3, fontSize: 10 }}>
      {states.map(state => {
        const agents = nodes.filter(n => !n.is_human && (n.state || '').toUpperCase() === state);
        if (agents.length === 0) return null;
        const color = theme.stateColors[state] || theme.stateColors[''];
        const label = state ? t(lang, `state.${state.toLowerCase()}`) : '—';
        return (
          <div key={state || '_empty'} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <span style={{ color, width: 8 }}>●</span>
            <span style={{ color, fontSize: 9, width: 50 }}>{label}</span>
            <div style={{ display: 'flex', gap: 3, flexWrap: 'wrap' }}>
              {agents.map(a => (
                <span key={a.address} style={{
                  background: color + '26',
                  border: `1px solid ${color}4d`,
                  borderRadius: 3,
                  padding: '1px 6px',
                  color,
                }}>
                  {a.nickname || a.agent_name || a.address.split('/').pop()}
                </span>
              ))}
            </div>
          </div>
        );
      })}
    </div>
  );
}
