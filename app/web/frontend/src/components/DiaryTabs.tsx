import type { AgentInfo } from "../types";
import { AGENT_COLORS } from "../types";

interface DiaryTabsProps {
  agents: AgentInfo[];
  activeTab: string;
  onTabChange: (tab: string) => void;
}

export function DiaryTabs({ agents, activeTab, onTabChange }: DiaryTabsProps) {
  return (
    <div className="flex gap-0 border-b border-border overflow-x-auto">
      <button
        className={`px-3 py-2 text-xs uppercase tracking-widest border-b-2 cursor-pointer bg-transparent ${
          activeTab === "all"
            ? "text-accent border-accent"
            : "text-text-dim border-transparent hover:text-text"
        }`}
        onClick={() => onTabChange("all")}
      >
        All
      </button>
      {agents.map((a, i) => (
        <button
          key={a.key}
          className={`px-3 py-2 text-xs uppercase tracking-widest border-b-2 cursor-pointer bg-transparent ${
            activeTab === a.key
              ? "border-accent"
              : "border-transparent hover:text-text"
          }`}
          style={{
            color:
              activeTab === a.key
                ? AGENT_COLORS[i % AGENT_COLORS.length]
                : undefined,
          }}
          onClick={() => onTabChange(a.key)}
        >
          {a.name}
        </button>
      ))}
    </div>
  );
}
