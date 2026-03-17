import { useEffect, useRef, useState } from "react";
import type { AgentInfo, DiaryEvent } from "../types";

const POLL_MS = 1500;

export function useDiary(agents: AgentInfo[]) {
  const [entries, setEntries] = useState<DiaryEvent[]>([]);
  const sinceRef = useRef<Record<string, number>>({});

  useEffect(() => {
    if (agents.length === 0) return;

    const poll = async () => {
      try {
        const fetches = agents.map(async (a) => {
          const since = sinceRef.current[a.key] ?? 0;
          const resp = await fetch(
            `/api/diary/${a.key}?since=${since}`
          );
          const data = await resp.json();
          const newEntries: DiaryEvent[] = (data.entries || []).map(
            (e: DiaryEvent) => ({
              ...e,
              agent_key: a.key,
              agent_name: a.name,
            })
          );
          // Update since to the latest timestamp
          if (newEntries.length > 0) {
            const maxTs = Math.max(...newEntries.map((e) => e.time));
            sinceRef.current[a.key] = maxTs;
          }
          return newEntries;
        });

        const results = await Promise.all(fetches);
        const allNew = results.flat();
        if (allNew.length > 0) {
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
