// Run with: node internal/ui/web_test.mjs
// Exercise the real frontend functions without a browser or filesystem cleanup.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const source = readFileSync(new URL('./web/app.js', import.meta.url), 'utf8');
function panel(platform) {
  const context = vm.createContext({
    URLSearchParams, Intl, setTimeout, clearTimeout,
    location: { search: '' },
    fetch: () => new Promise(() => {}),
    localStorage: { length: 0, getItem: () => null, setItem() {}, removeItem() {} },
    document: { documentElement: { dataset: { platform } }, querySelectorAll: () => [], addEventListener() {} },
    window: { matchMedia: () => ({ matches: false, addEventListener() {} }) },
  });
  vm.runInContext(source, context);
  return context;
}

for (const platform of ['windows', 'darwin']) {
  const context = panel(platform);
  const valid = value => vm.runInContext(`isValidRootInput(${JSON.stringify(value)})`, context);
  for (const input of ['~', '~/Projects']) assert.equal(valid(input), true);
  for (const input of ['Projects', 'C:relative', '']) assert.equal(valid(input), false);
  for (const input of ['C:\\Users\\Ana\\Projects', 'D:/Projects', '\\\\server\\share\\Projects', '~\\Projects']) {
    assert.equal(valid(input), platform === 'windows', input);
  }
  assert.equal(valid('/Users/ana/Projects'), platform === 'darwin');
  const path = platform === 'windows' ? 'D:\\Projects\\app\\node_modules' : '/Users/ana/file\\';
  const parts = Array.from(vm.runInContext(`splitPath(${JSON.stringify(path)})`, context));
  assert.equal(parts.join(''), path);
  assert.equal(parts[1], platform === 'windows' ? '\\node_modules' : '/file\\');
}

// A mode changed during a pending dry run must require a fresh preview.
const context = panel('windows');
const requests = [];
let resolveRequest;
context.mockAPI = (_path, body) => {
  requests.push(body.mode);
  return new Promise(resolve => { resolveRequest = resolve; });
};
context.mockModal = opts => { context.opts = opts; return Promise.resolve(null); };
vm.runInContext(`
api = mockAPI;
modal = mockModal;
selected.add('one');
itemIndex.set('one', {cat: {id:'user-caches', name:'User caches'}, item: {id:'one', size:10, risk:'safe', kind:'dir'}});
`, context);
await vm.runInContext('openClean()', context);
const nodes = Object.fromEntries(['clean-ack', 'ack-wrap', 'clean-est', 'ack-hint'].map(id => [id, { addEventListener() {} }]));
const icon = { classList: { toggle() {} } };
const radios = ['trash', 'delete'].map(value => ({ value, addEventListener(_event, handler) { this.change = handler; } }));
const ctx = {
  card: { querySelector: selector => selector === '.modal-icon' ? icon : nodes[selector.slice(1)], querySelectorAll: () => radios },
  confirmBtn: { focus() {} },
};
context.opts.onMount(ctx);
const first = context.opts.onConfirm(ctx);
radios[1].change();
nodes['clean-ack'].checked = true;
resolveRequest({ freed: 10, removed: 1, failed: [] });
assert.equal(await first, false);
const second = context.opts.onConfirm(ctx);
assert.deepEqual(requests, ['trash', 'delete']);
resolveRequest({ freed: 10, removed: 1, failed: [] });
assert.equal(await second, false);
assert.equal((await context.opts.onConfirm(ctx)).mode, 'delete');
assert.equal(requests.length, 2);
console.log('Frontend platform paths and cleanup mode race checks passed.');
