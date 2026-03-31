import { useEffect, useState, useRef, useCallback } from 'react';
import type { Network } from './types';
import { fetchNetwork, fetchTopology, type TapeFrame } from './api';
import { Graph, type EdgeMode, type Bullet } from './Graph';
import { TopBar } from './TopBar';
import { BottomBar } from './BottomBar';
import { getTheme, loadThemePreference, saveThemePreference } from './theme';
import { t } from './i18n';

function mailKey(sender: string, recipient: string) {
  return `${sender}\0${recipient}`;
}

/** Diff two network snapshots and return bullets for new mails. */
function diffMailBullets(prev: Network | null, next: Network, realNow: number): Bullet[] {
  if (!prev) return [];
  const prevMap = new Map<string, number>();
  for (const e of prev.mail_edges) prevMap.set(mailKey(e.sender, e.recipient), e.count);

  const bullets: Bullet[] = [];
  for (const e of next.mail_edges) {
    const key = mailKey(e.sender, e.recipient);
    const prevCount = prevMap.get(key) ?? 0;
    const delta = e.count - prevCount;
    if (delta > 0 && prevCount > 0) {
      const count = Math.min(delta, 8);
      for (let i = 0; i < count; i++) {
        bullets.push({
          src: e.sender,
          dst: e.recipient,
          born: realNow + i * 150 + Math.random() * 100,
        });
      }
    }
  }
  return bullets;
}

export type VizMode = 'live' | 'replay';

const SPEEDS = [1, 2, 5, 10, 25, 50, 100];

export default function App() {
  const [network, setNetwork] = useState<Network | null>(null);
  const [edgeMode, setEdgeMode] = useState<EdgeMode>('avatar');
  const [themeMode, setThemeMode] = useState<'dark' | 'light'>(loadThemePreference);
  const [bullets, setBullets] = useState<Bullet[]>([]);

  // Viz mode
  const [vizMode, setVizMode] = useState<VizMode>('live');
  const [speed, setSpeed] = useState(1);
  const [playing, setPlaying] = useState(false);
  const [replayTime, setReplayTime] = useState(0); // virtual clock (unix ms)
  const [tapeRange, setTapeRange] = useState<[number, number]>([0, 0]);

  // Replay engine refs (mutable, read by rAF loop)
  const replayRef = useRef({
    playing: false,
    speed: 1,
    virtualTime: 0,   // unix ms
    frameIndex: 0,     // current position in tape
    lastRealTime: 0,   // last rAF timestamp for delta calc
    tape: [] as TapeFrame[],
    prevNet: null as Network | null,
    lastDisplayedTime: 0,  // throttle setReplayTime
  });
  const replayAnimRef = useRef(0);

  // Live mode: use a ref for prev network to avoid stale closures
  const prevNetworkRef = useRef<Network | null>(null);

  // ── Live mode ────────────────────────────────────────────────

  const onNetworkUpdate = useCallback((net: Network) => {
    const prev = prevNetworkRef.current;
    const newBullets = diffMailBullets(prev, net, performance.now());
    prevNetworkRef.current = net;
    setNetwork(net);
    if (newBullets.length > 0) setBullets(newBullets);
  }, []); // no deps — uses ref, not state

  useEffect(() => {
    if (vizMode !== 'live') return;
    const poll = () => fetchNetwork().then(onNetworkUpdate).catch(console.error);
    poll();
    const id = setInterval(poll, 3000);
    return () => clearInterval(id);
  }, [onNetworkUpdate, vizMode]);

  // ── Replay rAF cleanup on unmount ────────────────────────────

  useEffect(() => {
    return () => cancelAnimationFrame(replayAnimRef.current);
  }, []);

  // ── Replay mode ──────────────────────────────────────────────

  const startReplayLoop = useCallback(() => {
    cancelAnimationFrame(replayAnimRef.current);

    const tick = (now: number) => {
      const r = replayRef.current;
      if (!r.playing) return; // stop loop when paused — no CPU waste

      const dt = now - r.lastRealTime;
      r.lastRealTime = now;
      r.virtualTime += dt * r.speed;

      // Clamp to tape range
      const lastT = r.tape[r.tape.length - 1]?.t ?? 0;
      if (r.virtualTime > lastT) {
        r.virtualTime = lastT;
        r.playing = false;
        setPlaying(false);
      }

      // Advance frame index and emit bullets for each crossed boundary
      let newBullets: Bullet[] = [];
      while (
        r.frameIndex < r.tape.length - 1 &&
        r.tape[r.frameIndex + 1].t <= r.virtualTime
      ) {
        r.frameIndex++;
        const frame = r.tape[r.frameIndex];
        const b = diffMailBullets(r.prevNet, frame.net, performance.now());
        newBullets = newBullets.concat(b);
        r.prevNet = frame.net;
        setNetwork(frame.net);
      }

      if (newBullets.length > 0) setBullets(newBullets);

      // Throttle setReplayTime — update only when displayed second changes
      const displayedSec = Math.floor(r.virtualTime / 1000);
      if (displayedSec !== r.lastDisplayedTime) {
        r.lastDisplayedTime = displayedSec;
        setReplayTime(r.virtualTime);
      }

      replayAnimRef.current = requestAnimationFrame(tick);
    };

    replayAnimRef.current = requestAnimationFrame(tick);
  }, []);

  const enterReplay = useCallback(async () => {
    let frames: TapeFrame[];
    try {
      frames = await fetchTopology();
    } catch (err) {
      console.error('Failed to load topology:', err);
      return;
    }
    if (frames.length === 0) return;

    const t0 = frames[0].t;
    const t1 = frames[frames.length - 1].t;
    setTapeRange([t0, t1]);
    setReplayTime(t0);
    setPlaying(true);
    setVizMode('replay');
    setNetwork(frames[0].net);

    const ref = replayRef.current;
    ref.tape = frames;
    ref.virtualTime = t0;
    ref.frameIndex = 0;
    ref.lastRealTime = performance.now();
    ref.playing = true;
    ref.speed = speed;
    ref.prevNet = null;
    ref.lastDisplayedTime = 0;

    startReplayLoop();
  }, [speed, startReplayLoop]);

  const exitReplay = useCallback(() => {
    cancelAnimationFrame(replayAnimRef.current);
    replayRef.current.playing = false;
    setVizMode('live');
    setPlaying(false);
    // Reset prev network so live mode doesn't fire stale diffs
    prevNetworkRef.current = null;
  }, []);

  const togglePlaying = useCallback(() => {
    const r = replayRef.current;
    if (!r.playing) {
      // If at end, restart
      if (r.frameIndex >= r.tape.length - 1) {
        r.virtualTime = r.tape[0]?.t ?? 0;
        r.frameIndex = 0;
        r.prevNet = null;
      }
      r.lastRealTime = performance.now();
      r.playing = true;
      setPlaying(true);
      startReplayLoop(); // restart rAF loop
    } else {
      r.playing = false;
      setPlaying(false);
      // rAF loop stops itself when r.playing is false
    }
  }, [startReplayLoop]);

  const seekTo = useCallback((unixMs: number) => {
    const r = replayRef.current;
    r.virtualTime = unixMs;
    let idx = 0;
    for (let i = r.tape.length - 1; i >= 0; i--) {
      if (r.tape[i].t <= unixMs) { idx = i; break; }
    }
    r.frameIndex = idx;
    r.prevNet = idx > 0 ? r.tape[idx - 1].net : null;
    r.lastRealTime = performance.now();
    setReplayTime(unixMs);
    setNetwork(r.tape[idx].net);
  }, []);

  const changeSpeed = useCallback((s: number) => {
    setSpeed(s);
    replayRef.current.speed = s;
  }, []);

  // ── Theme ────────────────────────────────────────────────────

  const theme = getTheme(themeMode);
  const lang = network?.lang ?? 'en';

  const toggleTheme = () => {
    const next = themeMode === 'dark' ? 'light' : 'dark';
    setThemeMode(next);
    saveThemePreference(next);
  };

  // ── Render ───────────────────────────────────────────────────

  if (!network) {
    return (
      <div style={{ background: theme.bg, color: theme.textDim, height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        {t(lang, 'connecting')}
      </div>
    );
  }

  return (
    <div style={{ background: theme.bg, height: '100vh', display: 'flex', flexDirection: 'column' }}>
      <TopBar
        lang={lang}
        theme={theme}
        vizMode={vizMode}
        playing={playing}
        speed={speed}
        speeds={SPEEDS}
        replayTime={replayTime}
        tapeRange={tapeRange}
        onEnterReplay={enterReplay}
        onExitReplay={exitReplay}
        onTogglePlaying={togglePlaying}
        onSeek={seekTo}
        onChangeSpeed={changeSpeed}
      />
      <div style={{ flex: 1, minHeight: 0 }}>
        <Graph
          network={network}
          edgeMode={edgeMode}
          theme={theme}
          bullets={bullets}
          vizMode={vizMode}
        />
      </div>
      <BottomBar
        network={network}
        edgeMode={edgeMode}
        lang={lang}
        theme={theme}
        themeMode={themeMode}
        onToggle={() => setEdgeMode(m => m === 'avatar' ? 'email' : 'avatar')}
        onToggleTheme={toggleTheme}
      />
    </div>
  );
}
