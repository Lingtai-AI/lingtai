import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import vm from 'node:vm';
import ts from 'typescript';

const graphSource = await readFile(new URL('./Graph.tsx', import.meta.url), 'utf8');
const instrumentedSource = `${graphSource}
module.exports.__testComputeLayout = computeLayout;
let __hexRgbCalls = [];
const __originalHexRgb = hexRgb;
hexRgb = (...args) => { __hexRgbCalls.push(args[0]); return __originalHexRgb(...args); };
module.exports.__hexRgbCalls = () => __hexRgbCalls.slice();
module.exports.__resetHexRgbCalls = () => { __hexRgbCalls = []; };
`;
const compiled = ts.transpileModule(instrumentedSource, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
}).outputText;

let activeHarness;
const react = {
  useRef: initial => activeHarness.useRef(initial),
  useEffect: (effect, deps) => activeHarness.useEffect(effect, deps),
  useCallback: (callback, deps) => activeHarness.useCallback(callback, deps),
};
const module = { exports: {} };
vm.runInNewContext(compiled, {
  module,
  exports: module.exports,
  require: name => name === 'react' ? react : name === 'react/jsx-runtime'
    ? { jsx: (type, props) => ({ type, props }) }
    : (() => { throw new Error(`Unexpected import: ${name}`); })(),
  requestAnimationFrame: callback => activeHarness.requestAnimationFrame(callback),
  cancelAnimationFrame: id => activeHarness.frames.delete(id),
  window: { devicePixelRatio: 1 },
  localStorage: {
    getItem: key => activeHarness.getItem(key),
    setItem: (key, value) => activeHarness.setItem(key, value),
  },
});
const { Graph, __testComputeLayout, __hexRgbCalls, __resetHexRgbCalls } = module.exports;

const sameDeps = (a, b) => a === b || (a && b && a.length === b.length && a.every((v, i) => Object.is(v, b[i])));

function harness() {
  const slots = [];
  const frames = new Map();
  const listeners = new Map();
  const storage = new Map();
  let getCount = 0;
  let hookIndex = 0;
  let nextFrame = 1;
  let trace = { arcs: [], fills: [], strokes: [], labels: [] };
  let path = [];
  const ctx = {
    setTransform() {}, clearRect() {}, save() {}, translate() {}, restore() {}, setLineDash() {},
    beginPath() { path = []; },
    moveTo(x, y) { path.push({ kind: 'move', x, y }); },
    lineTo(x, y) { path.push({ kind: 'line', x, y }); },
    arc(x, y, r) { path.push({ kind: 'arc', x, y, r }); trace.arcs.push({ x, y, r }); },
    fill() { trace.fills.push({ path: path.slice(), style: this.fillStyle }); },
    stroke() { trace.strokes.push({ path: path.slice(), style: this.strokeStyle }); },
    fillText(text, x, y) { trace.labels.push({ text, x, y }); },
  };
  const canvas = {
    clientWidth: 1000, clientHeight: 600, width: 0, height: 0, style: {},
    getContext: () => ctx,
    addEventListener: (name, fn) => listeners.set(name, fn),
    removeEventListener: name => listeners.delete(name),
  };
  const h = {
    frames, listeners, storage, canvas,
    get getCount() { return getCount; },
    get trace() { return trace; },
    getItem(key) { getCount++; return storage.get(key) ?? null; },
    setItem(key, value) { storage.set(key, value); },
    requestAnimationFrame(fn) { const id = nextFrame++; frames.set(id, fn); return id; },
    useRef(initial) {
      const i = hookIndex++;
      if (!slots[i]) slots[i] = { kind: 'ref', value: { current: initial } };
      return slots[i].value;
    },
    useCallback(callback, deps) {
      const i = hookIndex++;
      if (!slots[i] || !sameDeps(slots[i].deps, deps)) slots[i] = { kind: 'callback', deps, value: callback };
      return slots[i].value;
    },
    useEffect(effect, deps) {
      const i = hookIndex++;
      const old = slots[i];
      if (!old || !sameDeps(old.deps, deps)) {
        if (!old) slots[i] = { kind: 'effect', deps, cleanup: null };
        h.pending.push({ i, effect, deps });
      }
    },
    pending: [],
    render(props) {
      activeHarness = h;
      hookIndex = 0;
      h.pending = [];
      const element = Graph(props);
      element.props.ref.current = canvas;
      for (const { i, effect, deps } of h.pending) {
        const slot = slots[i];
        slot.cleanup?.();
        slot.deps = deps;
        slot.cleanup = effect() ?? null;
      }
      return element;
    },
    draw(now) {
      activeHarness = h;
      trace = { arcs: [], fills: [], strokes: [], labels: [] };
      const frame = frames.entries().next().value;
      assert.ok(frame, 'Graph registered a draw frame');
      frames.delete(frame[0]);
      frame[1](now);
      return trace;
    },
  };
  return h;
}

const node = (address, extra = {}) => ({
  address, agent_name: address, nickname: '', state: 'ACTIVE', alive: true,
  is_human: false, capabilities: [], ...extra,
});
const network = (nodes, extras = {}) => ({
  nodes, avatar_edges: [], contact_edges: [], mail_edges: [],
  stats: { active: nodes.length, idle: 0, stuck: 0, asleep: 0, suspended: 0, total_mails: 0 },
  lang: 'en', ...extras,
});
const theme = (mail = '#112233') => ({
  bg: '#000000', text: '#ddeeff', labelColorRgb: [200, 210, 220],
  edgeColors: { mail }, edgeOpacity: 0.65, amberRgb: [201, 122, 17],
  stateColors: { ACTIVE: '#00aa00', '': '#888888' },
});
const props = (net, extras = {}) => ({
  network: net, edgeMode: 'email', theme: theme(), bullets: [], vizMode: 'live', ...extras,
});
const labels = trace => Object.fromEntries(trace.labels.map(label => [label.text, [label.x, label.y - 17]]));

test('computeLayout keeps branch order, level spacing, human and orphan coordinates', () => {
  const nodes = [node('human', { is_human: true }), node('adminA'), node('adminB'), node('a1'), node('a2'), node('leaf'), node('b1'), node('ghost'), node('hchild')];
  const net = network(nodes, { avatar_edges: [
    { parent: 'adminA', child: 'a1' }, { parent: 'adminA', child: 'a2' },
    { parent: 'a1', child: 'leaf' }, { parent: 'adminB', child: 'b1' },
    { parent: 'missing', child: 'ghost' }, { parent: 'human', child: 'hchild' },
  ] });
  assert.deepEqual(JSON.parse(JSON.stringify(__testComputeLayout(net, 1000, 600))), {
    human: { x: 200, y: 300 }, adminA: { x: 1000 / 3, y: 260 }, adminB: { x: 1000 / 3, y: 340 },
    a1: { x: 1000 / 3 + 120, y: 210 }, a2: { x: 1000 / 3 + 120, y: 310 },
    leaf: { x: 1000 / 3 + 240, y: 210 }, b1: { x: 1000 / 3 + 120, y: 340 },
    ghost: { x: 1000 / 3 + 120, y: 250 }, hchild: { x: 1000 / 3 + 120, y: 350 },
  });
});

test('replay reads storage only for new dots and keeps saved, laid out, frozen and hidden positions', () => {
  const h = harness();
  const key = 'lingtai-viz-positions';
  const bullets = [];
  const base = network([node('root'), node('child')], { avatar_edges: [{ parent: 'root', child: 'child' }] });
  h.render(props(base, { bullets, vizMode: 'replay' }));
  assert.equal(h.getCount, 1, 'the initial new dots need saved-position lookup');
  let drawn = h.draw(100);
  const initial = labels(drawn);
  const rootStart = initial.root;
  h.listeners.get('mousedown')({ offsetX: rootStart[0], offsetY: rootStart[1], preventDefault() {} });
  h.listeners.get('mousemove')({ offsetX: rootStart[0] + 37, offsetY: rootStart[1] + 21 });
  h.listeners.get('mouseup')();
  const movedRoot = [rootStart[0] + 37, rootStart[1] + 21];

  h.storage.set(key, null);
  h.render(props(network([node('root', { nickname: 'updated' }), node('child')], {
    avatar_edges: [{ parent: 'root', child: 'child' }],
  }), { bullets, vizMode: 'replay' }));
  assert.equal(h.getCount, 1, 'an unchanged replay frame does not read storage');
  drawn = h.draw(120);
  assert.deepEqual(labels(drawn).updated, movedRoot, 'existing dots keep their dragged coordinates');

  h.render(props(network([node('root')], {
    avatar_edges: [{ parent: 'root', child: 'child' }],
  }), { bullets, vizMode: 'replay' }));
  drawn = h.draw(140);
  assert.ok(!labels(drawn).child, 'nodes absent from a replay frame stay hidden');
  assert.equal(h.getCount, 1);

  h.storage.set(key, JSON.stringify({ saved: { x: 111, y: 222 } }));
  const added = network([node('root'), node('child'), node('saved'), node('fallback')], {
    avatar_edges: [{ parent: 'root', child: 'child' }, { parent: 'root', child: 'fallback' }],
  });
  h.render(props(added, { bullets, vizMode: 'replay' }));
  assert.equal(h.getCount, 2, 'multiple new nodes share one saved-position read');
  drawn = h.draw(160);
  const positions = labels(drawn);
  assert.deepEqual(positions.root, movedRoot);
  assert.deepEqual(positions.saved, [111, 222], 'saved coordinates win over layout');
  assert.deepEqual(positions.fallback, [1000 / 3 + 120, 310], 'un-saved nodes use computeLayout');
  assert.ok(positions.child, 'a previously hidden dot reappears at its frozen position');
});

test('replay uses canvas center when a genuinely new dot is absent from its lazy layout', () => {
  const h = harness();
  const target = node('target');
  const batches = [[target], [target], []];
  const net = network([], {});
  Object.defineProperty(net, 'nodes', { get: () => batches.shift() ?? [target] });
  h.render(props(net, { vizMode: 'replay', bullets: [] }));
  assert.deepEqual(labels(h.draw(100)).target, [500, 300]);
  assert.equal(h.getCount, 1);
});

test('absent and empty hidden-node filters show the same dots and edges; hidden nodes remove incident edges', () => {
  const h = harness();
  const net = network([node('a'), node('b'), node('c')], {
    mail_edges: [
      { sender: 'a', recipient: 'b', direct: 1, cc: 0, bcc: 0 },
      { sender: 'b', recipient: 'c', direct: 1, cc: 0, bcc: 0 },
    ],
    contact_edges: [{ owner: 'a', target: 'c' }],
  });
  const bulletProps = [];
  h.render(props(net, { bullets: bulletProps }));
  const withoutFilter = h.draw(100);
  h.render(props(net, { bullets: bulletProps, filter: { hiddenNodes: new Set(), showDirect: true, showCC: true, showBCC: true } }));
  const emptyFilter = h.draw(120);
  assert.deepEqual(labels(emptyFilter), labels(withoutFilter));
  assert.deepEqual(emptyFilter.strokes.map(x => x.path), withoutFilter.strokes.map(x => x.path));

  h.render(props(net, { bullets: bulletProps, filter: { hiddenNodes: new Set(['b']), showDirect: true, showCC: true, showBCC: true } }));
  const hidden = h.draw(140);
  assert.deepEqual(Object.keys(labels(hidden)).sort(), ['a', 'c']);
  assert.equal(hidden.strokes.length, 1, 'only the non-incident contact edge remains');
});

test('mail edges and bullets reuse one current mail color in email and avatar modes', () => {
  for (const edgeMode of ['email', 'avatar']) {
    const h = harness();
    const net = network([node('a'), node('b')], {
      avatar_edges: [{ parent: 'a', child: 'b' }],
      mail_edges: [{ sender: 'a', recipient: 'b', direct: 1, cc: 0, bcc: 0 }],
    });
    let currentTheme = theme();
    const bullets = [{ src: 'a', dst: 'b', born: 0 }];
    const currentProps = () => props(net, { edgeMode, theme: currentTheme, bullets });
    h.render(currentProps());
    __resetHexRgbCalls();
    let drawn = h.draw(200);
    assert.equal(__hexRgbCalls().filter(hex => hex === '#112233').length, 1);
    assert.ok(drawn.strokes.some(x => x.style === (edgeMode === 'email' ? 'rgba(17,34,51,0.65)' : 'rgba(201,122,17,0.65)')));
    assert.ok(drawn.strokes.some(x => x.style === 'rgba(17,34,51,0.4)'), 'bullet trail retains its mail color');
    assert.ok(drawn.fills.some(x => x.style === 'rgba(17,34,51,0.9)'), 'bullet dot retains its mail color');

    currentTheme = theme('#445566');
    h.render(currentProps());
    __resetHexRgbCalls();
    drawn = h.draw(300);
    assert.equal(__hexRgbCalls().filter(hex => hex === '#445566').length, 1, 'theme changes reach the next draw');
    assert.ok(drawn.strokes.some(x => x.style === (edgeMode === 'email' ? 'rgba(68,85,102,0.65)' : 'rgba(201,122,17,0.65)')));
    assert.ok(drawn.fills.some(x => x.style === 'rgba(68,85,102,0.9)'));
  }
});
