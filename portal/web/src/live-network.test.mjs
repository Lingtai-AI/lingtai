import test from 'node:test';
import assert from 'node:assert/strict';
import { createLiveNetworkCoordinator } from './live-network.mjs';

function network(totalMails, id = 'agent-1', state = 'ACTIVE') {
  return {
    nodes: [{ id, address: id, state }],
    avatar_edges: [],
    contact_edges: [],
    mail_edges: totalMails === 0 ? [] : [{ sender: 'agent-1', recipient: 'agent-1', weight: totalMails }],
    stats: { active: 1, idle: 0, stuck: 0, asleep: 0, suspended: 0, total_mails: totalMails },
  };
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const flush = () => new Promise(resolve => setImmediate(resolve));

function manualSchedule() {
  let tick = () => {};
  return {
    schedule(fn) {
      tick = fn;
      return 1;
    },
    cancel() {},
    tick() {
      tick();
    },
  };
}

test('fast live start explicitly opts into mail=0 and keeps totals unavailable', async () => {
  const calls = [];
  const fast = [];
  const available = [];
  const clock = manualSchedule();
  const coordinator = createLiveNetworkCoordinator({
    fetchNetwork: options => {
      calls.push(options);
      return Promise.resolve(network(0));
    },
    onFastNetwork: value => fast.push(value),
    onFullNetwork: () => assert.fail('avatar mode must not issue a full-mail callback'),
    onMailAvailability: value => available.push(value),
    schedule: clock.schedule,
    cancelSchedule: clock.cancel,
  });

  coordinator.start('avatar');
  await flush();

  assert.equal(calls.length, 1);
  assert.equal(calls[0].includeMailEdges, false);
  assert.equal(fast[0].mail_edges.length, 0);
  assert.equal(fast[0].stats.total_mails, 0);
  assert.deepEqual(available, [false]);
  coordinator.stop();
});

test('both same-generation completion orders merge latest fast data with full mail data', async t => {
  for (const [label, fullFirst] of [['fast before full', false], ['full before fast', true]]) {
    await t.test(label, async () => {
      const calls = [];
      const pending = [];
      const fast = [];
      const full = [];
      const available = [];
      const clock = manualSchedule();
      const coordinator = createLiveNetworkCoordinator({
        fetchNetwork: options => {
          const request = deferred();
          calls.push(options);
          pending.push(request);
          return request.promise;
        },
        onFastNetwork: value => fast.push(value),
        onFullNetwork: value => full.push(value),
        onMailAvailability: value => available.push(value),
        schedule: clock.schedule,
        cancelSchedule: clock.cancel,
      });

      coordinator.start('email');
      assert.equal(calls.length, 2);
      assert.equal(calls[0].includeMailEdges, false);
      assert.equal(calls[1].includeMailEdges, true);

      if (fullFirst) {
        pending[1].resolve(network(3, 'full-result', 'IDLE'));
        await flush();
        assert.equal(full.length, 1, 'full response settles before fast in this generation');
        pending[0].resolve(network(0, 'fast-latest', 'ACTIVE'));
      } else {
        pending[0].resolve(network(0, 'fast-latest', 'ACTIVE'));
        await flush();
        pending[1].resolve(network(3, 'full-result', 'IDLE'));
      }
      await flush();

      const merged = fullFirst ? fast.at(-1) : full.at(-1);
      assert.equal(merged.nodes[0].id, 'fast-latest');
      assert.equal(merged.nodes[0].state, 'ACTIVE');
      assert.equal(merged.stats.total_mails, 3);
      assert.equal(merged.mail_edges.length, 1);
      assert.equal(available.at(-1), true);
      assert(available.slice(0, -1).every(value => value === false));
      coordinator.stop();
    });
  }
});

test('an empty full snapshot is loaded, then refreshed without using mail_edges.length as authority', async () => {
  const calls = [];
  const pending = [];
  const fast = [];
  const full = [];
  const available = [];
  const clock = manualSchedule();
  const coordinator = createLiveNetworkCoordinator({
    fetchNetwork: options => {
      const request = deferred();
      calls.push(options);
      pending.push(request);
      return request.promise;
    },
    onFastNetwork: value => fast.push(value),
    onFullNetwork: value => full.push(value),
    onMailAvailability: value => available.push(value),
    schedule: clock.schedule,
    cancelSchedule: clock.cancel,
  });

  coordinator.start('email');
  pending[0].resolve(network(0, 'fast-empty', 'IDLE'));
  await flush();
  pending[1].resolve(network(0, 'full-empty'));
  await flush();

  assert.equal(full.length, 1);
  assert.equal(full[0].nodes[0].id, 'fast-empty');
  assert.equal(full[0].nodes[0].state, 'IDLE');
  assert.equal(full[0].mail_edges.length, 0);
  assert.equal(full[0].stats.total_mails, 0);
  assert.equal(available.at(-1), true);

  clock.tick();
  assert.equal(calls.length, 4);
  assert.equal(available.at(-1), false);
  pending[2].resolve(network(0, 'fast-latest', 'ACTIVE'));
  await flush();
  assert.equal(fast.at(-1).nodes[0].id, 'fast-latest');
  assert.equal(fast.at(-1).mail_edges.length, 0);
  pending[3].resolve(network(2, 'full-refresh'));
  await flush();

  assert.equal(full.at(-1).nodes[0].id, 'fast-latest');
  assert.equal(full.at(-1).nodes[0].state, 'ACTIVE');
  assert.equal(full.at(-1).stats.total_mails, 2);
  assert.equal(full.at(-1).mail_edges.length, 1);
  assert.equal(available.at(-1), true);
  coordinator.stop();
});

test('in-flight ticks do not duplicate lanes; failed full stays unavailable and retries with new controllers', async () => {
  const calls = [];
  const pending = [];
  const available = [];
  const clock = manualSchedule();
  const coordinator = createLiveNetworkCoordinator({
    fetchNetwork: options => {
      const request = deferred();
      calls.push(options);
      pending.push(request);
      return request.promise;
    },
    onFastNetwork: () => {},
    onFullNetwork: () => {},
    onMailAvailability: value => available.push(value),
    schedule: clock.schedule,
    cancelSchedule: clock.cancel,
  });

  coordinator.start('email');
  clock.tick();
  assert.equal(calls.length, 2, 'an in-flight tick must not duplicate either lane');
  assert.notEqual(calls[0].signal, calls[1].signal);

  pending[0].resolve(network(0, 'fast-first'));
  pending[1].reject(new Error('full unavailable'));
  await flush();
  assert(available.every(value => value === false), 'a failed full request must not advertise mail totals');

  clock.tick();
  assert.equal(calls.length, 4, 'the next scheduled tick retries the failed full lane');
  assert.equal(new Set(calls.map(call => call.signal)).size, 4, 'new lanes use new controller identities');
  assert.equal(calls[2].includeMailEdges, false);
  assert.equal(calls[3].includeMailEdges, true);
  pending[2].resolve(network(0, 'fast-second'));
  pending[3].reject(new Error('full still unavailable'));
  await flush();
  coordinator.stop();
});

test('stop rejects both stale fast and full callbacks across logical unmount/mode transition', async () => {
  const oldFast = deferred();
  const oldFull = deferred();
  const newFast = deferred();
  const calls = [];
  const fast = [];
  const full = [];
  const clock = manualSchedule();
  const coordinator = createLiveNetworkCoordinator({
    fetchNetwork: options => {
      calls.push(options);
      if (calls.length === 1) return oldFast.promise;
      if (calls.length === 2) return oldFull.promise;
      return newFast.promise;
    },
    onFastNetwork: value => fast.push(value),
    onFullNetwork: value => full.push(value),
    onMailAvailability: () => {},
    schedule: clock.schedule,
    cancelSchedule: clock.cancel,
  });

  coordinator.start('email');
  coordinator.stop();
  coordinator.start('avatar');
  oldFast.resolve(network(0, 'stale-fast'));
  oldFull.resolve(network(9, 'stale-full'));
  newFast.resolve(network(0, 'current-fast'));
  await flush();

  assert.equal(full.length, 0, 'stale full callback must not cross the mode transition');
  assert.equal(fast.length, 1, 'only the current fast lane may callback after restart');
  assert.equal(fast[0].nodes[0].id, 'current-fast');
  coordinator.stop();
});
