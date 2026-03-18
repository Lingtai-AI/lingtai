import type { AgentInfo } from "../types";
import { AGENT_COLORS } from "../types";

interface DiaryTabsProps {
  agents: AgentInfo[];
  activeTab: string;
  onTabChange: (tab: string) => void;
}

export function DiaryTabs({ agents, activeTab, onTabChange }: DiaryTabsProps) {
  const activeAgent = agents.find((a) => a.key === activeTab);
  const activeIdx = agents.findIndex((a) => a.key === activeTab);
  const activeColor =
    activeTab === "all"
      ? undefined
      : AGENT_COLORS[activeIdx % AGENT_COLORS.length];

  return (
    <div className="flex items-center gap-2 px-3 py-1.5 border-b border-border">
      <span className="text-[10px] text-text-dim uppercase tracking-wider">
        Diary
      </span>
      <select
        value={activeTab}
        onChange={(e) => onTabChange(e.target.value)}
        className="px-2 py-1 text-xs border border-border rounded bg-bg text-text cursor-pointer"
        style={activeColor ? { color: activeColor } : undefined}
      >
        <option value="all">All agents</option>
        {agents.map((a) => (
          <option key={a.key} value={a.key}>
            {a.name}
          </option>
        ))}
      </select>
      {activeAgent && (
        <span
          className="text-[10px]"
          style={{ color: activeColor }}
        >
          :{activeAgent.port}
        </span>
      )}
    </div>
  );
}
