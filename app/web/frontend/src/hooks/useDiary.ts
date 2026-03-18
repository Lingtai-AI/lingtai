import { useEffect, useRef, useState } from "react";
import type { AgentInfo, DiaryEvent } from "../types";

const POLL_MS = 1500;

export function useDiary(agents: AgentInfo[]) {
  const [entries, setEntries] = useState<DiaryEvent[]>([]);
  const sinceRef = useRef(0);

  useEffect(() => {
    if (agents.length === 0) return;

    const poll = async () => {
      try {
        const since = sinceRef.current;
        const resp = await fetch(`/api/diary?since=${since}`);
        const data = await resp.json();

        const allNew: DiaryEvent[] = [];
        for (const [key, agentEntries] of Object.entries(data)) {
          const agent = agents.find((a) => a.key === key);
          if (!agent) continue;
          for (const e of agentEntries as DiaryEvent[]) {
            allNew.push({
              ...e,
              agent_key: key,
              agent_name: agent.name,
            });
          }
        }

        if (allNew.length > 0) {
          const maxTs = Math.max(...allNew.map((e) => e.time));
          sinceRef.current = maxTs;
          setEntries((prev) => {
            const combined = [...prev, ...allNew];
            combined.sort((a, b) => a.time - b.time);
            return combined;
          });
        }
      } catch {
        /* ignore */
      }
    };

    const id = setInterval(poll, POLL_MS);
    poll();
    return () => clearInterval(id);
  }, [agents]);

  return entries;
}
