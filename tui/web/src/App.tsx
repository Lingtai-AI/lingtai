import { useEffect, useState, useRef, useCallback } from 'react';
import type { Network } from './types';
import { fetchNetwork } from './api';
import { Graph, type EdgeMode, type Bullet } from './Graph';
import { TopBar } from './TopBar';
import { BottomBar } from './BottomBar';
import { getTheme, loadThemePreference, saveThemePreference } from './theme';
import { t } from './i18n';

/** Build a ledger key from sender→recipient */
function mailKey(sender: string, recipient: string) {
  return `${sender}\0${recipient}`;
}

export default function App() {
  const [network, setNetwork] = useState<Network | null>(null);
  const [edgeMode, setEdgeMode] = useState<EdgeMode>('avatar');
  const [themeMode, setThemeMode] = useState<'dark' | 'light'>(loadThemePreference);
  const [bullets, setBullets] = useState<Bullet[]>([]);

  // Mail edge ledger: key → last known count
  const ledgerRef = useRef<Map<string, number>>(new Map());

  const onNetworkUpdate = useCallback((net: Network) => {
    const ledger = ledgerRef.current;
    const newBullets: Bullet[] = [];
    const now = performance.now();

    for (const e of net.mail_edges) {
      const key = mailKey(e.sender, e.recipient);
      const prev = ledger.get(key) ?? 0;
      const delta = e.count - prev;

      if (delta > 0 && prev > 0) {
        // Only animate if we had a previous snapshot (skip initial load)
        // Stagger bullets so they don't overlap
        const count = Math.min(delta, 8); // cap at 8 simultaneous bullets
        for (let i = 0; i < count; i++) {
          newBullets.push({
            src: e.sender,
            dst: e.recipient,
            born: now + i * 150 + Math.random() * 100, // staggered 150ms apart + jitter
          });
        }
      }

      ledger.set(key, e.count);
    }

    setNetwork(net);
    if (newBullets.length > 0) {
      setBullets(newBullets);
    }
  }, []);

  useEffect(() => {
    const poll = () => fetchNetwork().then(onNetworkUpdate).catch(console.error);
    poll();
    const id = setInterval(poll, 3000);
    return () => clearInterval(id);
  }, [onNetworkUpdate]);

  const theme = getTheme(themeMode);
  const lang = network?.lang ?? 'en';

  const toggleTheme = () => {
    const next = themeMode === 'dark' ? 'light' : 'dark';
    setThemeMode(next);
    saveThemePreference(next);
  };

  if (!network) {
    return (
      <div style={{ background: theme.bg, color: theme.textDim, height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        {t(lang, 'connecting')}
      </div>
    );
  }

  return (
    <div style={{ background: theme.bg, height: '100vh', display: 'flex', flexDirection: 'column' }}>
      <TopBar lang={lang} theme={theme} />
      <div style={{ flex: 1, minHeight: 0 }}>
        <Graph network={network} edgeMode={edgeMode} theme={theme} bullets={bullets} />
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
