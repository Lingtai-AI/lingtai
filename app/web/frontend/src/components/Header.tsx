import type { AgentInfo } from "../types";

interface HeaderProps {
  agents: AgentInfo[];
  userPort: number;
}

export function Header({ agents, userPort }: HeaderProps) {
  const activeCount = agents.filter((a) => a.status === "active").length;
  return (
    <div className="flex items-center gap-3 px-5 py-2.5 bg-panel border-b border-border">
      <h1 className="text-base font-bold text-accent">StoAI</h1>
      <span className="text-xs text-text-dim">
        {agents.length} agent{agents.length !== 1 ? "s" : ""} · User
        mailbox :{userPort}
      </span>
      {activeCount > 0 && (
        <span className="text-xs text-emerald-400 ml-auto">
          ● {activeCount} active
        </span>
      )}
    </div>
  );
}
