import type { Network } from './types';

export interface LiveNetworkFetchOptions {
  signal?: AbortSignal;
  /** false is the explicitly incomplete mail=0 fast request; callers gate mail totals separately. */
  includeMailEdges?: boolean;
}

export interface LiveNetworkCoordinatorOptions {
  fetchNetwork: (options?: LiveNetworkFetchOptions) => Promise<Network>;
  onFastNetwork: (network: Network) => void;
  onFullNetwork: (network: Network) => void;
  onMailAvailability: (available: boolean) => void;
  schedule?: (fn: () => void, ms: number) => unknown;
  cancelSchedule?: (id: unknown) => void;
  intervalMs?: number;
}

export function createLiveNetworkCoordinator(
  options: LiveNetworkCoordinatorOptions,
): {
  start(mode?: 'avatar' | 'email'): void;
  stop(): void;
};
