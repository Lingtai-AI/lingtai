import type { Network } from './types';

export async function fetchNetwork(): Promise<Network> {
  const res = await fetch('/api/network');
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

export interface TapeFrame {
  t: number;    // unix milliseconds
  net: Network;
}

export async function fetchTopology(): Promise<TapeFrame[]> {
  const res = await fetch('/api/topology');
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}
