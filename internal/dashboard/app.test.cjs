const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const vm = require('node:vm');

function page() {
  class Node {
    constructor(value = '') { this.textContent = value; this.children = []; this.style = {}; this.classList = { toggle() {} }; }
    append(...nodes) { this.children.push(...nodes); }
    replaceChildren(...nodes) { this.children = nodes; }
    setAttribute() {}
    querySelector(tag) { return this.children[tag === 'span' ? 1 : 0]; }
  }
  const nodes = new Map(), streams = [], timers = [], listeners = {};
  let now = 0;
  const document = {
    getElementById(id) { if (!nodes.has(id)) nodes.set(id, new Node()); return nodes.get(id); },
    createElement: () => new Node(), createElementNS: () => new Node(),
    createTextNode: value => new Node(value), createDocumentFragment: () => new Node(),
    querySelectorAll: () => [], addEventListener: (name, fn) => { listeners[name] = fn; },
    hidden: false,
  };
  class EventSource {
    constructor() { this.handlers = {}; streams.push(this); }
    addEventListener(name, fn) { this.handlers[name] = fn; }
    close() { this.closed = true; }
    send(state) { this.handlers.state({ data: JSON.stringify(state) }); }
  }
  vm.runInNewContext(readFileSync(`${__dirname}/web/app.js`, 'utf8'), {
    document, EventSource, Intl, Date, performance: { now: () => now }, setInterval: fn => timers.push(fn),
  });
  return { nodes, streams, listeners, tick(value) { now = value; timers.forEach(fn => fn()); } };
}
function snapshot() {
  const time = new Date().toISOString();
  return { server_time: time, started: time, status: 'connected', environment: 'sandbox', stats: [], trades: [], predictions: [], points: [], signal_interval_seconds: 5, trade_count: 0, book_count: 0 };
}
test('hung stream is replaced and delayed events from old stream are ignored', () => {
  const p = page();
  p.streams[0].send(snapshot());
  p.tick(11000);
  assert.equal(p.streams.length, 2);
  assert.equal(p.streams[0].closed, true);
  p.streams[1].send({ ...snapshot(), environment: 'production' });
  p.streams[0].send(snapshot());
  assert.equal(p.nodes.get('environment').textContent, 'Production');
});
test('new empty session clears rows left over from previous session', () => {
  const p = page();
  p.streams[0].send(snapshot());
  p.nodes.get('trade-rows').replaceChildren('old trade');
  p.nodes.get('prediction-rows').replaceChildren('old prediction');
  p.streams[0].send({ ...snapshot(), started: '2026-01-01T00:00:00Z' });
  assert.equal(p.nodes.get('trade-rows').children[0].children[0].textContent, 'Ожидание сделок SBER');
  assert.match(p.nodes.get('prediction-rows').children[0].children[0].textContent, /Здесь появятся/);
});
test('returning to a stale tab reconnects', () => {
  const p = page();
  p.tick(5000);
  p.listeners.visibilitychange();
  assert.equal(p.streams.length, 2);
  assert.equal(p.streams[0].closed, true);
});

test('stale quotes remain visible and chart survives a long pause', () => {
  const p = page(), s = snapshot();
  s.book = { valid: false, time: s.server_time, mid: 300.005, spread: 0.01, bids: [{price: 300, quantity: 50}], asks: [{price: 300.01, quantity: 70}] };
  s.points = [{time: s.server_time, mid: 300.005}];
  p.streams[0].send(s);
  assert.notEqual(p.nodes.get('mid').textContent, '—');
  assert.equal(p.nodes.get('chart-empty').hidden, true);
  p.tick(3600000);
  assert.equal(p.nodes.get('chart-empty').hidden, true);
  assert.notEqual(p.nodes.get('mid').textContent, '—');
});
