// 墨韵灵台配色 — ink-wash + gold lacquer (matches lingtai.ai)

export interface Theme {
  bg: string;
  barBg: string;
  text: string;
  textDim: string;
  border: string;
  divider: string;
  stateColors: Record<string, string>;
  edgeColors: { avatar: string; mail: string };
  gold: string;
  goldRgb: [number, number, number];
  amberRgb: [number, number, number];
  labelColor: string;       // node name text on canvas
  labelColorRgb: [number, number, number];
  edgeOpacity: number;
}

// ── Dark mode (墨黑) ───────────────────────────────────────────────

const darkStateColors: Record<string, string> = {
  ACTIVE:    '#7dab8f',   // 竹青
  IDLE:      '#6b8fa8',   // 苍蓝
  STUCK:     '#c4956a',   // 赭石
  ASLEEP:    '#9b8fa0',   // 藕荷
  SUSPENDED: '#b85c5c',   // 朱砂
  '':        '#4a4845',   // 淡墨
};

export const darkTheme: Theme = {
  bg:         '#1a1a20',    // 墨黑
  barBg:      'rgba(13,13,15,0.95)',
  text:       '#e8e4df',    // 宣纸白
  textDim:    '#8a8680',    // 旧墨灰
  border:     '#2a2a30',    // 墨线
  divider:    'rgba(255,255,255,0.1)',
  stateColors: darkStateColors,
  edgeColors: { avatar: '#c49a6c', mail: '#7dab8f' },
  gold:       '#d4a853',
  goldRgb:    [212, 168, 83],
  amberRgb:   [196, 154, 108],
  labelColor:    '#d4a853',       // 金 — gold label
  labelColorRgb: [212, 168, 83],
  edgeOpacity:  0.45,
};

// ── Light mode (宣纸) ──────────────────────────────────────────────

const lightStateColors: Record<string, string> = {
  ACTIVE:    '#3d7a54',   // 深竹青
  IDLE:      '#3a6b85',   // 深苍蓝
  STUCK:     '#a06930',   // 深赭石
  ASLEEP:    '#7a6480',   // 深藕荷
  SUSPENDED: '#9b3a3a',   // 深朱砂
  '':        '#8a8680',   // 中墨灰
};

export const lightTheme: Theme = {
  bg:         '#f5f0e8',    // 宣纸色
  barBg:      'rgba(240,235,225,0.97)',
  text:       '#2a2520',    // 浓墨
  textDim:    '#6b6560',    // 暗墨灰
  border:     '#d5cfc5',    // 淡墨线
  divider:    'rgba(0,0,0,0.1)',
  stateColors: lightStateColors,
  edgeColors: { avatar: '#9a7040', mail: '#3d7a54' },
  gold:       '#a07820',
  goldRgb:    [160, 120, 32],
  amberRgb:   [154, 112, 64],
  labelColor:    '#3a3530',       // 浓墨 — dark ink label
  labelColorRgb: [58, 53, 48],
  edgeOpacity:  0.50,
};

// ── Theme selection ────────────────────────────────────────────────

const THEME_KEY = 'lingtai-viz-theme';

export function loadThemePreference(): 'dark' | 'light' {
  try {
    const v = localStorage.getItem(THEME_KEY);
    if (v === 'light' || v === 'dark') return v;
  } catch { /* ignore */ }
  // Respect OS preference
  if (typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: light)').matches) {
    return 'light';
  }
  return 'dark';
}

export function saveThemePreference(mode: 'dark' | 'light') {
  try {
    localStorage.setItem(THEME_KEY, mode);
  } catch { /* ignore */ }
}

export function getTheme(mode: 'dark' | 'light'): Theme {
  return mode === 'light' ? lightTheme : darkTheme;
}
