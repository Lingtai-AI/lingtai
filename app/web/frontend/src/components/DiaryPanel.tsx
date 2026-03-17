import { useEffect, useMemo, useRef, useState } from "react";
import type { AgentInfo, DiaryEvent } from "../types";
import { DiaryEntry } from "./DiaryEntry";
import { DiaryTabs } from "./DiaryTabs";

interface DiaryPanelProps {
  agents: AgentInfo[];
  entries: DiaryEvent[];
  addressToName: Record<string, string>;
}

export function DiaryPanel({ agents, entries, addressToName }: DiaryPanelProps) {
  const [activeTab, setActiveTab] = useState("all");
  const scrollRef = useRef<HTMLDivElement>(null);

  const filtered = useMemo(
    () =>
      activeTab === "all"
        ? entries
        : entries.filter((e) => e.agent_key === activeTab),
    [entries, activeTab]
  );

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [filtered]);

  return (
    <div className="flex-1 flex flex-col bg-panel-dark">
      <DiaryTabs
        agents={agents}
        activeTab={activeTab}
        onTabChange={setActiveTab}
      />
      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto p-3 text-xs text-text-muted"
      >
        {filtered.map((e, i) => (
          <DiaryEntry
            key={`${e.agent_key}-${e.time}-${i}`}
            event={e}
            agents={agents}
            addressToName={addressToName}
          />
        ))}
      </div>
    </div>
  );
}
