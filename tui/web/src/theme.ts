// 墨韵灵台配色 — inspired by Chinese ink-wash painting

// 节点状态色
export const inkStateColors: Record<string, string> = {
  ACTIVE:    '#7dab8f',   // 竹青
  IDLE:      '#6b8fa8',   // 苍蓝
  STUCK:     '#c4956a',   // 赭石
  ASLEEP:    '#9b8fa0',   // 藕荷
  SUSPENDED: '#b85c5c',   // 朱砂
  '':        '#4a4845',   // 淡墨
};

// 节点类型色
export const inkNodeTypeColors = {
  orchestrator: '#c49a6c',  // 琥珀
  human:        '#e8e4df',  // 宣纸白
  avatar:       '#7dab8f',  // 竹青
};

// 边缘色
export const inkEdgeColors = {
  avatar: '#7dab8f',  // 竹青实线
  mail:   '#6b8fa8',  // 苍蓝虚线
};

// 背景
export const inkBg = '#0d0d0f';  // 墨黑

// 文字色
export const ColorText = '#e8e4df';    // 宣纸白
export const ColorTextDim = '#8a8680'; // 旧墨灰

// 边框色
export const inkBorder = '#2a2a30';  // 墨线

// 向后兼容别名
export const stateColors = inkStateColors;
export const edgeColors = inkEdgeColors;
export const bg = inkBg;
