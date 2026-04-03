import { useEffect, useState } from 'react';
import type { Theme } from './theme';
import type { VizMode } from './App';
import type { EdgeMode } from './Graph';
import { t } from './i18n';

function formatTime(date: Date): string {
  const h = String(date.getHours()).padStart(2, '0');
  const m = String(date.getMinutes()).padStart(2, '0');
  const s = String(date.getSeconds()).padStart(2, '0');
  return `${h}:${m}:${s}`;
}

function formatDateTime(unixMs: number): string {
  const d = new Date(unixMs);
  const mon = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  const h = String(d.getHours()).padStart(2, '0');
  const m = String(d.getMinutes()).padStart(2, '0');
  const s = String(d.getSeconds()).padStart(2, '0');
  return `${mon}-${day} ${h}:${m}:${s}`;
}

function toDatetimeLocal(unixMs: number): string {
  const d = new Date(unixMs);
  const y = d.getFullYear();
  const mon = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  const h = String(d.getHours()).padStart(2, '0');
  const m = String(d.getMinutes()).padStart(2, '0');
  return `${y}-${mon}-${day}T${h}:${m}`;
}

function fromDatetimeLocal(val: string): number {
  return new Date(val).getTime();
}

function EdgeToggle({ edgeMode, lang, theme, onToggle }: {
  edgeMode: EdgeMode;
  lang: string;
  theme: Theme;
  onToggle: () => void;
}) {
  return (
    <div style={{
      display: 'flex',
      borderRadius: 4,
      overflow: 'hidden',
      border: `1px solid ${theme.border}`,
      flexShrink: 0,
    }}>
      {(['avatar', 'email'] as EdgeMode[]).map(mode => {
        const active = edgeMode === mode;
        const color = mode === 'avatar' ? theme.edgeColors.avatar : theme.edgeColors.mail;
        return (
          <button
            key={mode}
            onClick={active ? undefined : onToggle}
            style={{
              background: active ? color + '25' : 'transparent',
              border: 'none',
              borderRight: mode === 'avatar' ? `1px solid ${theme.border}` : 'none',
              padding: '2px 10px',
              cursor: active ? 'default' : 'pointer',
              color: active ? color : color + '55',
              fontSize: 10,
              letterSpacing: 0.5,
              transition: 'all 0.15s',
            }}
          >
            {t(lang, `edge.${mode}`)}
          </button>
        );
      })}
    </div>
  );
}

export function TopBar({ lang, theme, themeMode, vizMode, playing, speed, replayTime, tapeRange, viewRange, edgeMode, showFilter, onEnterReplay, onExitReplay, onTogglePlaying, onSeek, onChangeSpeed, onSetViewRange, onToggleTheme, onToggleEdgeMode, onToggleFilter }: {
  lang: string;
  theme: Theme;
  themeMode: 'dark' | 'light';
  vizMode: VizMode;
  playing: boolean;
  speed: number;
  replayTime: number;
  tapeRange: [number, number];
  viewRange: [number, number];
  edgeMode: EdgeMode;
  showFilter: boolean;
  onEnterReplay: () => void;
  onExitReplay: () => void;
  onTogglePlaying: () => void;
  onSeek: (unixMs: number) => void;
  onChangeSpeed: (s: number) => void;
  onSetViewRange: (range: [number, number]) => void;
  onToggleTheme: () => void;
  onToggleEdgeMode: () => void;
  onToggleFilter: () => void;
}) {
  const [now, setNow] = useState(() => new Date());
  const [trimming, setTrimming] = useState(false);

  useEffect(() => {
    if (vizMode !== 'live') return;
    const id = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(id);
  }, [vizMode]);

  const btnStyle = (active?: boolean): React.CSSProperties => ({
    background: active ? theme.stateColors['ACTIVE'] + '30' : 'transparent',
    border: `1px solid ${theme.border}`,
    borderRadius: 4,
    padding: '2px 8px',
    cursor: 'pointer',
    color: active ? theme.stateColors['ACTIVE'] : theme.textDim,
    fontSize: 10,
    letterSpacing: 0.5,
    flexShrink: 0,
  });

  const rightControls = (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
      <button onClick={onToggleTheme} style={btnStyle()}>
        {themeMode === 'dark' ? '☀' : '☽'}
      </button>
      <button
        onClick={onToggleFilter}
        style={{
          ...btnStyle(showFilter),
          fontSize: 14,
          lineHeight: 1,
          padding: '2px 6px',
        }}
      >
        ☰
      </button>
    </div>
  );

  if (vizMode === 'live') {
    return (
      <div style={{
        background: theme.barBg,
        borderBottom: `1px solid ${theme.border}`,
        padding: '6px 16px',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        flexShrink: 0,
        userSelect: 'none',
      }}>
        {/* Left: live indicator + edge mode */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <span style={{
              display: 'inline-block',
              width: 6,
              height: 6,
              borderRadius: '50%',
              background: theme.stateColors['ACTIVE'],
              boxShadow: `0 0 4px ${theme.stateColors['ACTIVE']}`,
            }} />
            <span style={{
              fontSize: 10,
              fontWeight: 600,
              letterSpacing: 1,
              color: theme.stateColors['ACTIVE'],
            }}>
              {t(lang, 'topbar.live')}
            </span>
          </div>
          <EdgeToggle edgeMode={edgeMode} lang={lang} theme={theme} onToggle={onToggleEdgeMode} />
        </div>

        {/* Center: replay button */}
        <button onClick={onEnterReplay} style={btnStyle()}>
          {'⏮ ' + t(lang, 'topbar.replay')}
        </button>

        {/* Right: clock + theme + hamburger */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <div style={{
            fontFamily: 'monospace',
            fontSize: 12,
            color: theme.textDim,
            letterSpacing: 1,
          }}>
            {formatTime(now)}
          </div>
          {rightControls}
        </div>
      </div>
    );
  }

  // ── Replay mode ──────────────────────────────────────────────

  const [v0, v1] = viewRange;
  const [tape0, tape1] = tapeRange;
  const duration = v1 - v0 || 1;
  const progress = (replayTime - v0) / duration;
  const isTrimmed = v0 !== tape0 || v1 !== tape1;

  const dtInputStyle: React.CSSProperties = {
    background: 'transparent',
    border: `1px solid ${theme.border}`,
    borderRadius: 4,
    padding: '1px 4px',
    color: theme.gold,
    fontSize: 10,
    fontFamily: 'monospace',
    outline: 'none',
    width: 145,
    colorScheme: 'dark',
  };

  return (
    <div style={{
      background: theme.barBg,
      borderBottom: `1px solid ${theme.border}`,
      padding: '6px 16px',
      display: 'flex',
      flexDirection: 'column',
      gap: trimming ? 6 : 0,
      flexShrink: 0,
      userSelect: 'none',
    }}>
      {/* Main row */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        {/* Left: back to live + edge mode */}
        <button onClick={onExitReplay} style={btnStyle()}>
          {'● ' + t(lang, 'topbar.live')}
        </button>
        <EdgeToggle edgeMode={edgeMode} lang={lang} theme={theme} onToggle={onToggleEdgeMode} />

        <div style={{ width: 1, height: 16, background: theme.border, flexShrink: 0 }} />

        {/* Play / Pause */}
        <button onClick={onTogglePlaying} style={btnStyle(playing)}>
          {playing ? '⏸' : '▶'}
        </button>

        {/* Speed input */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 3 }}>
          <input
            type="text"
            inputMode="numeric"
            value={speed}
            onChange={e => {
              const raw = e.target.value.replace(/[^0-9]/g, '');
              if (raw === '') return;
              const v = Math.max(1, Math.min(9999, Number(raw)));
              onChangeSpeed(v);
            }}
            style={{
              background: 'transparent',
              border: `1px solid ${theme.border}`,
              borderRadius: 4,
              padding: '2px 4px',
              color: theme.stateColors['ACTIVE'],
              fontSize: 11,
              fontFamily: 'monospace',
              width: 48,
              textAlign: 'right' as const,
              outline: 'none',
            }}
          />
          <span style={{ fontSize: 10, color: theme.textDim }}>×</span>
        </div>

        {/* Scrubber */}
        <input
          type="range"
          min={v0}
          max={v1}
          step={1000}
          value={replayTime}
          onChange={e => onSeek(Number(e.target.value))}
          style={{
            flex: 1,
            accentColor: theme.stateColors['ACTIVE'],
            cursor: 'pointer',
            height: 4,
          }}
        />

        {/* Progress % */}
        <span style={{ fontSize: 10, color: theme.textDim, minWidth: 32, textAlign: 'right' }}>
          {Math.round(progress * 100)}%
        </span>

        {/* Trim toggle */}
        <button
          onClick={() => setTrimming(!trimming)}
          style={btnStyle(trimming || isTrimmed)}
          title="Set start/end time"
        >
          ✂
        </button>

        {/* Virtual clock */}
        <div style={{
          fontFamily: 'monospace',
          fontSize: 12,
          color: theme.gold,
          letterSpacing: 1,
          minWidth: 110,
          textAlign: 'right',
        }}>
          {formatDateTime(replayTime)}
        </div>

        {rightControls}
      </div>

      {/* Trim row */}
      {trimming && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, paddingLeft: 4 }}>
          <span style={{ fontSize: 9, color: theme.textDim }}>from</span>
          <input
            type="datetime-local"
            value={toDatetimeLocal(v0)}
            min={toDatetimeLocal(tape0)}
            max={toDatetimeLocal(v1)}
            onChange={e => {
              const ms = fromDatetimeLocal(e.target.value);
              if (!isNaN(ms)) onSetViewRange([ms, v1]);
            }}
            style={dtInputStyle}
          />
          <span style={{ fontSize: 9, color: theme.textDim }}>to</span>
          <input
            type="datetime-local"
            value={toDatetimeLocal(v1)}
            min={toDatetimeLocal(v0)}
            max={toDatetimeLocal(tape1)}
            onChange={e => {
              const ms = fromDatetimeLocal(e.target.value);
              if (!isNaN(ms)) onSetViewRange([v0, ms]);
            }}
            style={dtInputStyle}
          />
          {isTrimmed && (
            <button
              onClick={() => onSetViewRange([tape0, tape1])}
              style={{ ...btnStyle(), fontSize: 9 }}
              title="Reset to full range"
            >
              reset
            </button>
          )}
        </div>
      )}
    </div>
  );
}
