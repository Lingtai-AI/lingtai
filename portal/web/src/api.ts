import type { Network, AgentNode, AvatarEdge, ContactEdge, MailEdge, NetworkStats } from './types';

export async function fetchNetwork(): Promise<Network> {
  const res = await fetch('/api/network');
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

export interface TapeFrame {
  t: number;    // unix milliseconds
  net: Network;
}

// --- Chunked replay API ---

export interface ChunkInfo {
  start: number;
  end: number;
  frames: number;
}

export interface ReplayManifest {
  tape_start: number;
  tape_end: number;
  chunks: ChunkInfo[];
}

interface FrameDelta {
  nodes?: AgentNode[];
  avatar_edges?: AvatarEdge[];
  avatar_edges_removed?: [string, string][];
  contact_edges?: ContactEdge[];
  contact_edges_removed?: [string, string][];
  mail?: MailEdge[];
  mail_removed?: [string, string][];
  stats?: NetworkStats;
}

interface ReplayFrame {
  t: number;
  net?: Network;
  d?: FrameDelta;
}

export interface ReplayChunk {
  v?: number;
  start: number;
  end: number;
  keyframe_interval: number;
  frames: ReplayFrame[];
}

export async function fetchManifest(): Promise<ReplayManifest> {
  const res = await fetch('/api/topology/manifest');
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

export async function fetchChunk(startMs: number): Promise<ReplayChunk> {
  const res = await fetch(`/api/topology/chunk?start=${startMs}`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

/**
 * Reconstruct full TapeFrame[] from a delta-encoded ReplayChunk.
 * Applies deltas on top of keyframes to produce complete snapshots.
 */
export function reconstructFrames(chunk: ReplayChunk): TapeFrame[] {
  const frames: TapeFrame[] = [];
  let current: Network | null = null;

  for (const rf of chunk.frames) {
    if (rf.net) {
      // Keyframe — use as-is
      current = rf.net;
    } else if (current) {
      // Delta — apply changes to a copy of the current network
      const net: Network = structuredClone(current);

      if (rf.d) {
        // Apply node changes
        if (rf.d.nodes) {
          const nodeMap = new Map(net.nodes.map(n => [n.address, n]));
          for (const n of rf.d.nodes) {
            if ((n.state as string) === '__REMOVED__') {
              nodeMap.delete(n.address);
            } else {
              nodeMap.set(n.address, n);
            }
          }
          net.nodes = Array.from(nodeMap.values());
        }

        // Apply avatar edge changes
        if (rf.d.avatar_edges || rf.d.avatar_edges_removed) {
          const avatarMap = new Map(
            net.avatar_edges.map(e => [`${e.parent}\0${e.child}`, e])
          );
          for (const [parent, child] of rf.d.avatar_edges_removed ?? []) {
            avatarMap.delete(`${parent}\0${child}`);
          }
          for (const e of rf.d.avatar_edges ?? []) {
            avatarMap.set(`${e.parent}\0${e.child}`, e);
          }
          net.avatar_edges = Array.from(avatarMap.values());
        }

        // Apply contact edge changes
        if (rf.d.contact_edges || rf.d.contact_edges_removed) {
          const contactMap = new Map(
            net.contact_edges.map(e => [`${e.owner}\0${e.target}`, e])
          );
          for (const [owner, target] of rf.d.contact_edges_removed ?? []) {
            contactMap.delete(`${owner}\0${target}`);
          }
          for (const e of rf.d.contact_edges ?? []) {
            contactMap.set(`${e.owner}\0${e.target}`, e);
          }
          net.contact_edges = Array.from(contactMap.values());
        }

        // Apply mail edge changes
        if (rf.d.mail || rf.d.mail_removed) {
          const mailMap = new Map(
            net.mail_edges.map(e => [`${e.sender}\0${e.recipient}`, e])
          );
          for (const [sender, recipient] of rf.d.mail_removed ?? []) {
            mailMap.delete(`${sender}\0${recipient}`);
          }
          for (const e of rf.d.mail ?? []) {
            mailMap.set(`${e.sender}\0${e.recipient}`, e);
          }
          net.mail_edges = Array.from(mailMap.values());
        }

        // Apply stats
        if (rf.d.stats) {
          net.stats = rf.d.stats;
        }
      }

      current = net;
    } else {
      continue; // skip delta before first keyframe (shouldn't happen)
    }

    frames.push({ t: rf.t, net: current! });
  }

  return frames;
}
