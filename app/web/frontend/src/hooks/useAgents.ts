import { useEffect, useState } from "react";
import type { AgentInfo } from "../types";

export function useAgents() {
  const [agents, setAgents] = useState<AgentInfo[]>([]);

  useEffect(() => {
    fetch("/api/agents")
      .then((r) => r.json())
      .then((data) => setAgents(data))
      .catch(() => {});
  }, []);

  const keyToName: Record<string, string> = {};
  const addressToName: Record<string, string> = {};
  for (const a of agents) {
    keyToName[a.key] = a.name;
    addressToName[a.address] = a.name;
  }

  return { agents, keyToName, addressToName };
}
