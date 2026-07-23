import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const api = readFileSync(new URL('../src/api.ts', import.meta.url), 'utf8');
const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8');

assert.match(api, /networkEndpoint\(includeMailEdges = false\)/, 'API exposes explicit networkEndpoint helper');
assert.match(api, /includeMailEdges \? '\/api\/network\?mail=1' : '\/api\/network'/, 'full-mail fetch uses ?mail=1 opt-in');
assert.match(app, /fetchNetwork\(\{ signal: controller\.signal \}\)/, 'live first paint/poll uses default no-mail endpoint');
assert.match(app, /fetchNetwork\(\{ includeMailEdges: true, signal: controller\.signal \}\)/, 'email edge mode loads full mail edges explicitly');
assert.match(app, /if \(vizMode !== 'live' \|\| edgeMode !== 'email'\)/, 'email-edge load only runs in live email mode');
assert.match(app, /if \(\(network\?\.mail_edges\.length \?\? 0\) > 0 \|\| mailLoadRef\.current\) return;/, 'email-edge load is cached and non-overlapping');
assert.match(app, /preserveLoadedMailEdges\(net, prev\)/, 'default polls preserve cached mail edges after load');
