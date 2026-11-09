// Run with: node --test internal/website/controller_behavior_test.cjs
// Execute the shipped controller against a small DOM/window model. Geometry
// and native RPC completion are independent so a page-only fill cannot pass
// for successful Windows fullscreen.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const vm = require('node:vm');

const source = readFileSync(`${__dirname}/controller.js`, 'utf8');

function fixture(options = {}) {
  let clock = 0;
  let observer;
  let siteFullscreen = false;
  const calls = [];
  const apiCalls = [];
  const navigations = [];
  const hintTargets = [];
  const windowListeners = {};
  const documentListeners = {};
  const listen = (map, name, fn) => (map[name] ||= []).push(fn);
  const emit = (map, name, event = {}) => (map[name] || []).forEach(fn => fn(event));
  const document = {
    addEventListener: (name, fn) => listen(documentListeners, name, fn),
    querySelectorAll: name => {
      if (name === 'video') return options.noVideo ? [] : [video];
      if (name.startsWith('a[href]')) return hintTargets.filter(e => e.tagName !== 'ICON');
      if (name === '#youku-dashboard icon[com="icon"]') return hintTargets.filter(e => e.tagName === 'ICON');
      if (name === '#youku-dashboard play-rate [data-val]') return (options.rates || [1, 1.25, 1.5, 2]).map(rate => ({
        getAttribute: name => name === 'data-val' ? String(rate) : null,
      }));
      return [];
    },
    querySelector: name => name === '#ykPlayer .video-layer video' && !options.noProgram ? video : null,
    elementsFromPoint: (x, y) => {
      if (!options.hints) return [header, video, player, body, html];
      return hintTargets.filter(e => {
        const r = e.getBoundingClientRect();
        return x >= r.left && x <= r.right && y >= r.top && y <= r.bottom;
      });
    },
    elementFromPoint: (x, y) => document.elementsFromPoint(x, y)[0],
    createElement: tag => { const el = element(null, 0, 0); el.tagName = tag.toUpperCase(); return el; },
    fullscreenElement: null,
  };
  const window = {
    innerWidth: 1280, innerHeight: 720,
    location: {
      hostname: options.host || 'v.youku.com',
      href: 'https://v.youku.com/v_show/test.html',
      assign: href => navigations.push(href),
    },
    addEventListener: (name, fn) => listen(windowListeners, name, fn),
    dispatchEvent: event => emit(windowListeners, event.type, event),
    getComputedStyle: el => ({
      display: el.dashboardControl && !window.videoPlayer?.holding ? 'none' : 'block',
      visibility: 'visible', opacity: '1', pointerEvents: 'auto',
    }),
  };
  window.top = window;
  document.defaultView = window;
  if (!options.noBridge) {
    window.tinyplayWebsiteSetFullscreen = active => {
      calls.push(active);
      return options.bridge ? options.bridge(active) : Promise.resolve();
    };
  }

  function element(parent, width, height) {
    const el = {
      nodeType: 1, tagName: 'DIV', parentElement: parent, ownerDocument: document,
      parentNode: parent, children: [], style: {}, attrs: {}, clicks: 0,
      getAttribute(name) { return this.attrs[name] ?? null; },
      setAttribute(name, value) { this.attrs[name] = value; },
      hasAttribute(name) { return Object.hasOwn(this.attrs, name); },
      appendChild(child) { this.children.push(child); child.parentNode = child.parentElement = this; },
      removeChild(child) { this.children.splice(this.children.indexOf(child), 1); child.parentNode = child.parentElement = null; },
      querySelector: () => null,
      matches() { return this.tagName === 'A' || this.tagName === 'BUTTON'; },
      closest() { return this.tagName === 'A' ? this : null; },
      focus() {},
      click() { this.dispatchEvent(new window.MouseEvent('click')); },
      removeAttribute() { this.style = {}; },
      contains(el) {
        for (let node = el; node; node = node.parentElement) if (node === this) return true;
        return false;
      },
      getBoundingClientRect() {
        const filled = this === video && (siteFullscreen || (!options.rejectPin &&
          (this.style.position === 'fixed' || player.style.position === 'fixed')));
        const w = filled ? window.innerWidth : width;
        const h = filled ? window.innerHeight : height;
        const top = this.top || 0, left = this.left || 0;
        return { width: w, height: h, top, left, right: left + w, bottom: top + h };
      },
      dispatchEvent(event) {
        event.target ||= this;
        emit(documentListeners, event.type, event);
        if (event.type === 'click') { this.clicks++; this.onClick?.(); }
        if (options.siteKeys && event.type === 'keydown') {
          if (event.key === 'f') siteFullscreen = !siteFullscreen;
          if (event.key === 'Escape') siteFullscreen = false;
        }
        return true;
      },
    };
    if (parent) parent.children.push(el);
    return el;
  }
  const html = element(null, 1280, 1500);
  const body = element(html, 1280, 1500);
  const header = element(body, 1280, 60);
  const player = element(body, 840, 500);
  const video = element(player, 800, 450);
  video.tagName = 'VIDEO';
  video.readyState = 4;
  video.playbackRate = 1;
  document.documentElement = html;
  document.body = body;
  const timer = (fn, ms) => queueMicrotask(() => { clock += ms; fn(); });
  window.setTimeout = timer;
  window.MouseEvent = class {
    constructor(type, opts) { Object.assign(this, opts, { type }); }
    preventDefault() { this.defaultPrevented = true; }
  };
  if (options.youku) {
    const state = { fullscreen: false, webFullscreen: false };
    window.videoPlayer = {
      getPlayerState: () => ({ ...state }),
      // The real wrapper discards the browser Promise; the adapter must call
      // its underlying documentElement entry point to handle denial safely.
      enterFullscreen() { throw new Error('Do not discard the browser Promise'); },
      webFullscreen(enter) {
        apiCalls.push(`web:${enter}`);
        state.webFullscreen = enter;
        siteFullscreen = enter && !options.apiIgnored;
      },
      holdDashboard() { this.holding = true; apiCalls.push('hold'); },
      releaseDashboard() { this.holding = false; apiCalls.push('release'); },
    };
    if (options.youkuSpeed) {
      window.videoPlayer.adPlaying = false;
      window.videoPlayer._player = { ui: {
        setSettings(key, rate) {
          apiCalls.push(`${key}:${rate}`);
          if (options.speedThrows) throw new Error('Changed private method');
          if (options.speedRejects) return Promise.reject(new Error('Rejected rate'));
          if (options.speedPending) return new Promise(() => {});
          if (!options.speedIgnored) video.playbackRate = rate;
          if (options.speedReverts) timer(() => { video.playbackRate = 1; }, 100);
          if (options.adStarts) timer(() => { window.videoPlayer.adPlaying = true; }, 100);
        },
      } };
    }
    html.requestFullscreen = () => {
      apiCalls.push('native');
      if (!options.nativeAllowed) return Promise.reject(new Error('No user activation'));
      document.fullscreenElement = html;
      state.fullscreen = siteFullscreen = true;
      emit(documentListeners, 'fullscreenchange');
      return Promise.resolve();
    };
    document.exitFullscreen = () => {
      document.fullscreenElement = null;
      state.fullscreen = siteFullscreen = false;
      emit(documentListeners, 'fullscreenchange');
      return Promise.resolve();
    };
  }
  const context = vm.createContext({
    window, document, location: window.location,
    innerWidth: window.innerWidth, innerHeight: window.innerHeight,
    // Advance effect-oracle time without real sleeps.
    Date: { now: () => clock },
    setTimeout: timer,
    Event: class { constructor(type) { this.type = type; } },
    KeyboardEvent: class {
      constructor(type, opts) { Object.assign(this, opts, { type, isTrusted: false }); }
    },
    MutationObserver: class {
      constructor(fn) { observer = fn; }
      observe() {}
    },
  });
  vm.runInContext(source, context);
  return {
    calls, apiCalls, navigations, document, window, video, header,
    action: (action, extra) => window.__tinyplayWebsite.handle({ action, ...extra }),
    addHintTarget(tag, href) {
      const el = element(body, 80, 30);
      el.tagName = tag;
      el.top = 80 + hintTargets.length * 50;
      if (href !== undefined) { el.attrs.href = href; el.href = new URL(href, window.location.href).href; }
      hintTargets.push(el);
      return el;
    },
    emitDocument: (name, event) => emit(documentListeners, name, event),
    emitWindow: name => emit(windowListeners, name),
    removePlayer() { video.parentElement = null; player.parentElement = null; observer(); },
  };
}

test('Youku CSS fill promotes Windows and phone exit restores both layers', async () => {
  const f = fixture();
  assert.equal((await f.action('fullscreen_enter')).status, 'fullscreen_enter');
  assert.deepEqual(f.calls, [true]);
  assert.equal(f.document.fullscreenElement, null, 'CSS emits no DOM fullscreen event');
  assert.equal(f.video.getBoundingClientRect().width, 1280);
  assert.equal(f.header.style.visibility, 'hidden');
  assert.equal((await f.action('fullscreen_exit')).status, 'fullscreen_exit');
  assert.deepEqual(f.calls, [true, false]);
  assert.equal(f.video.getBoundingClientRect().width, 800);
  assert.equal(f.header.style.visibility, undefined);
  assert.equal(f.document.body.style.overflow, undefined);
});

test('enter waits for native completion and reports native failure honestly', async () => {
  let resolve;
  const f = fixture({ bridge: () => new Promise(done => { resolve = done; }) });
  let completed = false;
  const pending = f.action('fullscreen_enter').then(result => { completed = true; return result; });
  for (let i = 0; i < 100 && !resolve; i++) await Promise.resolve();
  assert.deepEqual(f.calls, [true]);
  assert.equal(completed, false);
  resolve();
  assert.equal((await pending).ok, true);

  const failed = fixture({ bridge: () => Promise.reject(new Error('native failure')) });
  assert.equal((await failed.action('fullscreen_enter')).error, 'effect_unconfirmed');
});

test('physical Escape restores CSS and native window; later exit is safe', async () => {
  const f = fixture();
  await f.action('fullscreen_enter');
  f.emitDocument('keydown', { key: 'Escape', isTrusted: true });
  assert.deepEqual(f.calls, [true, false]);
  assert.equal(f.video.getBoundingClientRect().width, 800);
  assert.equal((await f.action('fullscreen_exit')).ok, true);
  assert.equal(f.calls.at(-1), false);
});

test('site shortcut fill promotes host and repeated enter does not toggle it off', async () => {
  const f = fixture({ host: 'www.bilibili.com', siteKeys: true });
  await f.action('fullscreen_enter');
  await f.action('fullscreen_enter');
  assert.deepEqual(f.calls, [true, true]);
  assert.equal(f.video.style.position, undefined, 'site shortcut needs no CSS pin');
  assert.equal(f.video.getBoundingClientRect().width, 1280);
  await f.action('fullscreen_exit');
  assert.equal(f.calls.at(-1), false);
  assert.equal(f.video.getBoundingClientRect().width, 800);
});

test('site native fullscreen exit also clears a previous phone CSS pin', async () => {
  const f = fixture();
  await f.action('fullscreen_enter');
  f.document.fullscreenElement = f.video;
  f.emitDocument('fullscreenchange');
  f.document.fullscreenElement = null;
  f.emitDocument('fullscreenchange');
  assert.equal(f.calls.at(-1), false);
  assert.equal(f.video.getBoundingClientRect().width, 800);
});

test('native exit must take effect before reporting success', async () => {
  const f = fixture();
  f.document.fullscreenElement = f.video;
  f.document.exitFullscreen = () => Promise.resolve(); // ignored by browser
  assert.equal((await f.action('fullscreen_exit')).error, 'effect_unconfirmed');
  assert.deepEqual(f.calls, []);
  f.document.exitFullscreen = () => {
    f.document.fullscreenElement = null;
    f.emitDocument('fullscreenchange');
    return Promise.resolve();
  };
  assert.equal((await f.action('fullscreen_exit')).ok, true);
  assert.equal(f.calls.at(-1), false);
});

test('navigation and SPA player removal release the native window', async () => {
  const f = fixture();
  await f.action('fullscreen_enter');
  f.emitWindow('pagehide');
  assert.equal(f.calls.at(-1), false);
  const spa = fixture();
  await spa.action('fullscreen_enter');
  spa.removePlayer();
  assert.equal(spa.calls.at(-1), false);
  assert.equal(spa.header.style.visibility, undefined);
});

test('missing media or failed page fill never promotes Windows', async () => {
  for (const options of [{ noVideo: true }, { rejectPin: true }]) {
    const f = fixture(options);
    assert.equal((await f.action('fullscreen_enter')).ok, false);
    assert.deepEqual(f.calls, []);
  }
});

test('hosts without the optional Windows bridge retain CSS fullscreen', async () => {
  const f = fixture({ noBridge: true });
  assert.equal((await f.action('fullscreen_enter')).ok, true);
  assert.equal((await f.action('fullscreen_exit')).ok, true);
});

test('Youku native fullscreen runs before site web-fullscreen or CSS', async () => {
  const f = fixture({ youku: true, nativeAllowed: true });
  assert.equal((await f.action('fullscreen_enter')).ok, true);
  assert.deepEqual(f.apiCalls, ['native']);
  assert.equal(f.document.fullscreenElement, f.document.documentElement);
  assert.equal(f.video.style.position, undefined);
  assert.equal(f.calls.at(-1), true);
  assert.equal((await f.action('fullscreen_exit')).ok, true);
  assert.equal(f.document.fullscreenElement, null);
  assert.equal(f.calls.at(-1), false);
});

test('Youku user-activation denial uses its own web-fullscreen API and keeps controls', async () => {
  const f = fixture({ youku: true });
  assert.equal((await f.action('fullscreen_enter')).ok, true);
  assert.deepEqual(f.apiCalls, ['native', 'web:true']);
  assert.equal(f.video.style.position, undefined);
  assert.equal(f.header.style.visibility, undefined, 'no generic CSS was used');
  await f.action('fullscreen_enter');
  assert.deepEqual(f.apiCalls, ['native', 'web:true'], 'repeat enter is idempotent');
  assert.equal((await f.action('fullscreen_exit')).ok, true);
  assert.equal(f.apiCalls.at(-1), 'web:false');
  assert.equal(f.video.getBoundingClientRect().width, 800);
  assert.equal(f.calls.at(-1), false);
});

test('changed Youku API that claims success without resizing falls back to CSS', async () => {
  const f = fixture({ youku: true, apiIgnored: true });
  assert.equal((await f.action('fullscreen_enter')).ok, true);
  assert.deepEqual(f.apiCalls, ['native', 'web:true', 'web:false']);
  assert.equal(f.video.style.position, 'fixed');
  assert.equal(f.calls.at(-1), true);
});

test('physical Escape also releases Youku web-fullscreen', async () => {
  const f = fixture({ youku: true });
  await f.action('fullscreen_enter');
  f.emitDocument('keydown', { key: 'Escape', isTrusted: true });
  assert.equal(f.apiCalls.at(-1), 'web:false');
  assert.equal(f.video.getBoundingClientRect().width, 800);
  assert.equal(f.calls.at(-1), false);
});

test('Youku placeholder anchors activate their click handler instead of assigning href', async () => {
  for (const href of ['#', '#tab', 'javascript:void(0)', 'javaScript:;']) {
    const f = fixture({ hints: true });
    const link = f.addHintTarget('A', href);
    link.attrs.target = '_blank';
    const entered = await f.action('hint_enter');
    assert.equal(entered.ok, true);
    const result = await f.action('hint_label', { label: entered.labels[0] });
    assert.equal(result.ok, true);
    assert.equal(link.clicks, 1, href);
    assert.deepEqual(f.navigations, [], href);
  }
});

test('genuine video result URLs still navigate in the singleton WebView', async () => {
  const f = fixture({ hints: true });
  const link = f.addHintTarget('A', 'https://v.youku.com/video?vid=example');
  const entered = await f.action('hint_enter');
  await f.action('hint_label', { label: entered.labels[0] });
  assert.deepEqual(f.navigations, [link.href]);
});

test('Youku custom icons become Hint targets after dashboard is held and release on selection', async () => {
  const f = fixture({ hints: true, youku: true });
  const icon = f.addHintTarget('ICON');
  icon.dashboardControl = true;
  const result = await f.action('hint_enter');
  assert.equal(result.ok, true);
  assert.equal(result.labels.length, 1);
  assert.deepEqual(f.apiCalls, ['hold']);
  assert.equal(f.window.videoPlayer.holding, true);
  assert.equal((await f.action('hint_label', { label: result.labels[0] })).ok, true);
  assert.equal(icon.clicks, 1);
  assert.deepEqual(f.apiCalls, ['hold', 'release']);
  assert.equal(f.window.videoPlayer.holding, false);
});

test('Youku dashboard hold is released on cancel, empty results, and navigation', async () => {
  for (const action of ['hint_exit', 'pagehide', 'empty']) {
    const f = fixture({ hints: true, youku: true });
    if (action !== 'empty') f.addHintTarget('A', '#');
    const result = await f.action('hint_enter');
    if (action === 'empty') assert.equal(result.error, 'no_targets');
    else if (action === 'pagehide') f.emitWindow('pagehide');
    else await f.action(action);
    assert.deepEqual(f.apiCalls, ['hold', 'release']);
    assert.equal(f.window.videoPlayer.holding, false);
  }
});

test('Youku absolute speed uses the observed private settings function on both desktop hosts', async () => {
  for (const noBridge of [false, true]) {
    const f = fixture({ youku: true, youkuSpeed: true, noBridge });
    const result = await f.action('speed', { text: '1.5' });
    assert.equal(result.status, 'speed');
    assert.equal(result.ok, true);
    assert.deepEqual(f.apiCalls, ['rate:1.5']);
    assert.equal(f.video.playbackRate, 1.5);
  }
});

test('missing or changed Youku speed function fails without generic playbackRate fallback', async () => {
  for (const mutate of [p => { delete p._player; }, p => { p._player.ui.setSettings = undefined; }]) {
    const f = fixture({ youku: true, youkuSpeed: true });
    mutate(f.window.videoPlayer);
    assert.equal((await f.action('speed', { text: '1.5' })).error, 'action_unavailable');
    assert.equal(f.video.playbackRate, 1);
    assert.deepEqual(f.apiCalls, []);
  }
});

test('throwing, rejected, pending, ignored and reverted Youku speed changes report failure', async () => {
  for (const behavior of ['speedThrows', 'speedRejects', 'speedPending', 'speedIgnored', 'speedReverts']) {
    const f = fixture({ youku: true, youkuSpeed: true, [behavior]: true });
    assert.equal((await f.action('speed', { text: '1.5' })).error, 'effect_unconfirmed', behavior);
    assert.equal(f.video.playbackRate, 1, behavior);
    assert.deepEqual(f.apiCalls, ['rate:1.5']);
  }
});

test('Youku requests only menu-offered rates and does not change adverts', async () => {
  const advert = fixture({ youku: true, youkuSpeed: true });
  advert.window.videoPlayer.adPlaying = true;
  const unsupported = fixture({ youku: true, youkuSpeed: true, rates: [1, 1.25] });
  const absentMenu = fixture({ youku: true, youkuSpeed: true, rates: [] });
  for (const f of [advert, unsupported, absentMenu]) {
    assert.equal((await f.action('speed', { text: '1.5' })).error, 'action_unavailable');
    assert.equal(f.video.playbackRate, 1);
    assert.deepEqual(f.apiCalls, []);
  }
});

test('Youku speed needs a programme video and confirms the same session', async () => {
  const absent = fixture({ youku: true, youkuSpeed: true, noProgram: true });
  assert.equal((await absent.action('speed', { text: '1.5' })).error, 'no_player');
  const changed = fixture({ youku: true, youkuSpeed: true });
  changed.window.videoPlayer._player.ui.setSettings = () => {
    changed.video.playbackRate = 1.5;
    changed.window.videoPlayer = { getPlayerState: () => ({}) };
  };
  assert.equal((await changed.action('speed', { text: '1.5' })).error, 'effect_unconfirmed');
  const advert = fixture({ youku: true, youkuSpeed: true, adStarts: true });
  assert.equal((await advert.action('speed', { text: '1.5' })).error, 'effect_unconfirmed');
});

test('other allowed sites retain their generic absolute-speed path', async () => {
  const f = fixture({ host: 'www.bilibili.com' });
  assert.equal((await f.action('speed', { text: '1.25' })).ok, true);
  assert.equal(f.video.playbackRate, 1.25);
  assert.deepEqual(f.apiCalls, []);
});
