/* TinyPlay website controller — injected into the desktop website WebView.
 * Phone clients never evaluate caller-provided script; they only enqueue
 * allowlisted actions that the native shell maps onto these handlers.
 */
(function () {
  'use strict';
  if (window.__tinyplayWebsite && window.__tinyplayWebsite.__version >= 12) {
    return;
  }

  var HOST = location.hostname || '';

  // Per-site tables are the only per-site knowledge this file carries. Both
  // are small and additive: an unmatched host falls back to the generic
  // (site-agnostic) path below, it never breaks. Prefer adding a table row
  // here over hunting a site's DOM/CSS, which drifts on every redesign.
  var SEARCH_URL_TEMPLATES = [
    { test: /(^|\.)bilibili\.com$/i, url: 'https://search.bilibili.com/all?keyword={q}' },
    { test: /(^|\.)iqiyi\.com$/i, url: 'https://www.iqiyi.com/search?q={q}' },
    { test: /(^|\.)v\.qq\.com$/i, url: 'https://v.qq.com/x/search/?q={q}' },
    { test: /(^|\.)youku\.com$/i, url: 'https://so.youku.com/search/q_{q}' },
  ];
  var LOGIN_URLS = [
    { test: /(^|\.)bilibili\.com$/i, url: 'https://passport.bilibili.com/' },
    { test: /(^|\.)iqiyi\.com$/i, url: 'https://www.iqiyi.com/iframe/loginreg?show_back=1' },
    { test: /(^|\.)v\.qq\.com$/i, url: 'https://v.qq.com/s/videoplus/host' },
    { test: /(^|\.)youku\.com$/i, url: 'https://account.youku.com/' },
  ];
  // Generic login-trigger text is retained only as a fallback for a site that
  // has no fixed route in the table above.
  var LOGIN_TEXT = /^(登录|登陆|登录注册|注册\/登录|login|log in|sign in)$/i;
  // Per-site keyboard fallbacks. Synthetic KeyboardEvents are untrusted
  // (isTrusted === false); every use is effect-oracle-confirmed and falls
  // through to the generic path on failure — never a blind claimed success.
  // Keys sit *behind* the standard media / CSS-pin APIs, never as step 1.
  //   bilibili: Space play/pause, F web-fullscreen (user-reported; oracle-gated)
  //   iqiyi:    Space play/pause, F web-fullscreen (user-reported; oracle-gated)
  var SITE_KEYS = [
    { test: /(^|\.)bilibili\.com$/i, keys: { play_pause: ' ', fullscreen: 'f' } },
    { test: /(^|\.)iqiyi\.com$/i, keys: { play_pause: ' ', fullscreen: 'f' } },
  ];

  function siteKey(action) {
    for (var i = 0; i < SITE_KEYS.length; i++) {
      if (SITE_KEYS[i].test.test(HOST)) {
        var key = SITE_KEYS[i].keys[action];
        return typeof key === 'string' && key ? key : null;
      }
    }
    return null;
  }

  function findURLTemplate(table) {
    for (var i = 0; i < table.length; i++) {
      if (table[i].test.test(HOST)) return table[i].url;
    }
    return null;
  }

  var hintState = {
    active: false,
    map: Object.create(null),
    root: null,
    style: null,
  };

  function setNativeValue(el, value) {
    if (!el) return false;
    var proto = el instanceof HTMLTextAreaElement
      ? HTMLTextAreaElement.prototype
      : HTMLInputElement.prototype;
    var desc = Object.getOwnPropertyDescriptor(proto, 'value');
    if (desc && desc.set) {
      desc.set.call(el, value);
    } else {
      el.value = value;
    }
    try {
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
    } catch (_) {}
    return true;
  }

  function ownerWindow(el) {
    return (el && el.ownerDocument && el.ownerDocument.defaultView) || window;
  }

  // Lightweight geometry check used by search/login probes (not Hint).
  function isVisible(el) {
    if (!el || el.nodeType !== 1) return false;
    var view = ownerWindow(el);
    var style;
    try { style = view.getComputedStyle(el); } catch (_) { return false; }
    if (!style || style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') {
      return false;
    }
    var rect = el.getBoundingClientRect();
    if (rect.width < 2 || rect.height < 2) return false;
    if (rect.bottom < 0 || rect.right < 0 || rect.top > view.innerHeight || rect.left > view.innerWidth) {
      return false;
    }
    return true;
  }

  // ── Hint target reachability ────────────────────────────────────────────
  // A Hint label is only useful when a user could actually click that pixel
  // right now. Geometry alone is not enough: player chrome often sits off-
  // screen, under opacity:0 skins, behind inert overlays, or clipped by
  // overflow:hidden ancestors while still reporting a non-zero rect.

  function parseOpacity(style) {
    var o = parseFloat(style && style.opacity);
    return isFinite(o) ? o : 1;
  }

  // True when the element and every ancestor is paint-present and not
  // explicitly non-interactive (inert / aria-hidden / disabled chain).
  function isInteractivelyPresent(el) {
    var doc = el.ownerDocument || document;
    var view = ownerWindow(el);
    var node = el;
    while (node && node.nodeType === 1) {
      if (node.hasAttribute && node.hasAttribute('inert')) return false;
      if (node.getAttribute && node.getAttribute('aria-hidden') === 'true') return false;
      if (node.disabled === true) return false;
      if (node.getAttribute && node.getAttribute('aria-disabled') === 'true') return false;
      var style;
      try { style = view.getComputedStyle(node); } catch (_) { return false; }
      if (!style) return false;
      if (style.display === 'none' || style.visibility === 'hidden') return false;
      if (parseOpacity(style) < 0.05) return false;
      // pointer-events:none on the target itself means it cannot be the hit;
      // on an ancestor it only blocks when no descendant re-enables, which
      // elementsFromPoint already models — skip only the leaf case here.
      if (node === el && style.pointerEvents === 'none') return false;
      if (node === doc.documentElement || node === doc.body) break;
      node = node.parentElement;
    }
    return true;
  }

  // Clip the element's box against overflow-hidden/auto/scroll ancestors so
  // off-panel player controls do not get labels just because their unclipped
  // bounding rect still intersects the viewport. Coordinates are relative to
  // the element's owner window (iframe-local when nested).
  function visibleClientRect(el) {
    var doc = el.ownerDocument || document;
    var view = ownerWindow(el);
    var rect = el.getBoundingClientRect();
    var top = rect.top;
    var left = rect.left;
    var bottom = rect.bottom;
    var right = rect.right;
    var node = el.parentElement;
    while (node && node.nodeType === 1) {
      // The root scrollers (<html>/<body>) are never real clip surfaces for our
      // purposes — the viewport clamp below already bounds the box to what is on
      // screen. Break *before* clipping against them: some SPA video sites (e.g.
      // iQIYI) lay content out with fixed/absolute positioning and leave <body>
      // with a collapsed clientHeight of 0, so clipping against it would shrink
      // every element to a zero-height box and drop all Hint targets.
      if (node === doc.documentElement || node === doc.body) break;
      var style;
      try { style = view.getComputedStyle(node); } catch (_) { break; }
      var overflow = style ? (style.overflow + style.overflowX + style.overflowY) : '';
      if (overflow && /auto|scroll|hidden|clip/.test(overflow)) {
        var cr = node.getBoundingClientRect();
        var cWidth = node.clientWidth || cr.width;
        var cHeight = node.clientHeight || cr.height;
        // Skip a degenerate (zero-sized) clip box: a collapsed wrapper is not a
        // real clipper, and using it would shrink the target to nothing.
        if (cWidth > 0 && cHeight > 0) {
          var cTop = cr.top;
          var cLeft = cr.left;
          var cBottom = cr.top + cHeight;
          var cRight = cr.left + cWidth;
          if (top < cTop) top = cTop;
          if (left < cLeft) left = cLeft;
          if (bottom > cBottom) bottom = cBottom;
          if (right > cRight) right = cRight;
        }
      }
      node = node.parentElement;
    }
    var vh = view.innerHeight || 0;
    var vw = view.innerWidth || 0;
    if (top < 0) top = 0;
    if (left < 0) left = 0;
    if (bottom > vh) bottom = vh;
    if (right > vw) right = vw;
    if (right - left < 2 || bottom - top < 2) return null;
    return { top: top, left: left, bottom: bottom, right: right, width: right - left, height: bottom - top };
  }

  // Label / click sample point inside the visible box (matches enterHints).
  function labelPoint(vis) {
    return {
      x: vis.left + Math.min(vis.width, 24) / 2,
      y: vis.top + Math.min(vis.height, 24) / 2,
    };
  }

  // True when the paint/hit stack at the label point reaches el (el itself or
  // a descendant). Occluded controls, offscreen slides, and zero-opacity
  // skins fail here even when getBoundingClientRect looks fine.
  function isHitTestReachable(el, vis) {
    if (!vis) return false;
    var doc = el.ownerDocument || document;
    if (typeof doc.elementsFromPoint !== 'function') {
      // Without elementsFromPoint, fall back to geometry only (already clipped).
      return true;
    }
    var pt = labelPoint(vis);
    var stack;
    try {
      stack = doc.elementsFromPoint(pt.x, pt.y);
    } catch (_) {
      return false;
    }
    if (!stack || !stack.length) return false;
    for (var i = 0; i < stack.length; i++) {
      var hit = stack[i];
      if (!hit || hit.nodeType !== 1) continue;
      // Skip our own Hint overlay if it is somehow still mounted.
      if (hit.id === 'tinyplay-hint-root' || (hit.classList && hit.classList.contains('tinyplay-hint-label'))) {
        continue;
      }
      // Reachable when the top non-overlay hit is the element or sits inside it.
      // An ancestor hit means the click landed outside el (sibling/padding).
      if (hit === el || el.contains(hit)) return true;
      return false;
    }
    return false;
  }

  // Full Hint gate: present, in viewport after clipping, hit-test reachable.
  function isHintReachable(el) {
    if (!el || el.nodeType !== 1) return false;
    if (!isInteractivelyPresent(el)) return false;
    var vis = visibleClientRect(el);
    if (!vis) return false;
    return isHitTestReachable(el, vis);
  }

  function queryDeep(selectors, root) {
    root = root || document;
    for (var i = 0; i < selectors.length; i++) {
      try {
        var el = root.querySelector(selectors[i]);
        if (el && isVisible(el)) return el;
      } catch (_) {}
    }
    return null;
  }

  function genericSearchInput() {
    return queryDeep([
      'input[type="search"]',
      'input[name="search"]',
      'input[name="q"]',
      'input[name="wd"]',
      'input[placeholder*="搜索"]',
      'input[placeholder*="Search" i]',
      'input[aria-label*="搜索"]',
      'input[aria-label*="search" i]',
      'form[role="search"] input[type="text"]',
      'form[action*="search" i] input[type="text"]',
      'input[type="text"]',
    ]);
  }

  function findSearchInput() {
    return genericSearchInput();
  }

  function submitSearch(input) {
    if (!input) return false;
    input.focus();
    var form = input.form || input.closest('form');
    if (form) {
      if (typeof form.requestSubmit === 'function') {
        try {
          form.requestSubmit();
          return true;
        } catch (_) {}
      }
      try {
        form.submit();
        return true;
      } catch (_) {}
    }
    // Fallback for sites without a confirmed SEARCH_URL_TEMPLATES entry: try
    // a nearby submit-shaped button before resorting to a synthetic Enter.
    var btn = queryDeep([
      'button[type="submit"]',
      '.search-button',
      'button[aria-label*="搜索"]',
      'button[aria-label*="search" i]',
    ], form || document);
    if (btn) {
      btn.click();
      return true;
    }
    // Last resort: Enter key event (untrusted, may no-op on some sites).
    try {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true }));
      input.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true }));
    } catch (_) {}
    return true;
  }

  // ── Effect oracles ─────────────────────────────────────────────────────
  // A waterfall step only counts as success when its oracle confirms a real
  // state change; otherwise the next step runs. Without this, an ignored
  // untrusted key dispatch would report success and the cascade would never
  // advance — "honest degradation" requires observing the effect.

  // Resolve true as soon as check() passes, false after timeoutMs.
  function settle(check, timeoutMs) {
    return new Promise(function (resolve) {
      var start = Date.now();
      (function tick() {
        var ok = false;
        try { ok = !!check(); } catch (_) {}
        if (ok) return resolve(true);
        if (Date.now() - start >= timeoutMs) return resolve(false);
        setTimeout(tick, 80);
      })();
    });
  }

  // Check once *after* the delay (not until-true): for values that read back
  // correctly immediately, the failure mode is the site's own controller
  // reverting them moments later.
  function confirmAfter(delayMs, check) {
    return new Promise(function (resolve) {
      setTimeout(function () {
        var ok = false;
        try { ok = !!check(); } catch (_) {}
        resolve(ok);
      }, delayMs);
    });
  }

  function failed(code) {
    return { ok: false, status: 'error', error: code };
  }

  // Dispatch an untrusted key at the page (blurring text inputs first so the
  // site's global hotkey handler sees it, not a text field).
  function dispatchKey(key) {
    var active = document.activeElement;
    if (active && (active.tagName === 'INPUT' || active.tagName === 'TEXTAREA' || active.isContentEditable)) {
      try { active.blur(); } catch (_) {}
    }
    var code, keyCode;
    if (key === ' ') {
      code = 'Space';
      keyCode = 32;
    } else if (/^[a-z]$/i.test(key)) {
      code = 'Key' + key.toUpperCase();
      keyCode = key.toUpperCase().charCodeAt(0);
    } else {
      code = key;
      keyCode = 0;
    }
    var opts = { key: key, code: code, keyCode: keyCode, which: keyCode, bubbles: true, cancelable: true };
    var target = document.body || document.documentElement;
    try {
      target.dispatchEvent(new KeyboardEvent('keydown', opts));
      target.dispatchEvent(new KeyboardEvent('keyup', opts));
      return true;
    } catch (_) {
      return false;
    }
  }

  function primaryVideo() {
    var videos = Array.prototype.slice.call(document.querySelectorAll('video'));
    var best = null;
    var bestArea = 0;
    for (var i = 0; i < videos.length; i++) {
      var v = videos[i];
      if (!isVisible(v) && v.readyState < 1) continue;
      var r = v.getBoundingClientRect();
      var area = Math.max(0, r.width) * Math.max(0, r.height);
      if (area >= bestArea) {
        best = v;
        bestArea = area;
      }
    }
    return best;
  }

  // Standard HTMLMediaElement API first — even the custom MSE/EME shells on
  // commercial sites render into a real <video>, and the media API is the only
  // path whose effect we can read back. A verified site key (SITE_KEYS) is the
  // fallback for shells whose own controller fights the direct call.
  function togglePlayPause() {
    var video = primaryVideo();
    if (!video) return failed('no_player');
    var wasPaused = video.paused;
    if (wasPaused) {
      var p = video.play();
      if (p && typeof p.catch === 'function') p.catch(function () {});
    } else {
      video.pause();
    }
    var flipped = function () { return video.paused !== wasPaused; };
    return settle(flipped, 450).then(function (viaAPI) {
      if (viaAPI) return { ok: true, status: 'play_pause' };
      var key = siteKey('play_pause');
      if (key && dispatchKey(key)) {
        return settle(flipped, 600).then(function (viaKey) {
          return viaKey ? { ok: true, status: 'play_pause' } : failed('effect_unconfirmed');
        });
      }
      return failed('effect_unconfirmed');
    });
  }

  function seekBy(text) {
    var video = primaryVideo();
    if (!video) return failed('no_player');
    var delta = parseFloat(text);
    if (!isFinite(delta)) return failed('bad_delta');
    var next = video.currentTime + delta;
    if (isFinite(video.duration)) next = Math.max(0, Math.min(video.duration, next));
    else next = Math.max(0, next);
    video.currentTime = next;
    // currentTime reads back as the seek target while seeking, so this passes
    // immediately in the normal case; it only fails when a custom shell
    // forcibly snaps playback elsewhere (e.g. a narrow live seekable range).
    var landed = function () {
      return video.seeking || Math.abs(video.currentTime - next) <= 3;
    };
    return settle(landed, 450).then(function (done) {
      return done ? { ok: true, status: 'seek' } : failed('effect_unconfirmed');
    });
  }

  function setSpeed(text) {
    var video = primaryVideo();
    if (!video) return failed('no_player');
    var rate = parseFloat(text);
    if (!isFinite(rate) || rate <= 0) return failed('bad_rate');
    video.playbackRate = rate;
    return confirmAfter(350, function () {
      return Math.abs(video.playbackRate - rate) < 0.01;
    }).then(function (kept) {
      return kept ? { ok: true, status: 'speed' } : failed('effect_unconfirmed');
    });
  }

  function scrollPage(direction) {
    var delta = window.innerHeight * 0.9;
    window.scrollBy({ top: direction === 'up' ? -delta : delta, behavior: 'smooth' });
    return { ok: true, status: direction === 'up' ? 'scroll_up' : 'scroll_down' };
  }

  function findLoginTrigger() {
    var candidates = document.querySelectorAll('a, button, [role="button"], div, span');
    for (var i = 0; i < candidates.length; i++) {
      var el = candidates[i];
      if (el.children.length > 1) continue;
      var text = (el.textContent || '').trim();
      if (!text || text.length > 10 || !LOGIN_TEXT.test(text)) continue;
      if (isVisible(el)) return el;
    }
    return null;
  }

  function doLogin() {
    var url = findURLTemplate(LOGIN_URLS);
    if (url) {
      window.location.assign(url);
      return { ok: true, status: 'login' };
    }
    var trigger = findLoginTrigger();
    if (trigger) {
      trigger.click();
      return { ok: true, status: 'login' };
    }
    return { ok: false, status: 'error', error: 'no_login' };
  }

  // Climbs from <video> to the nearest ancestor whose box is meaningfully
  // larger than the video's own box — i.e. it wraps extra chrome (controls,
  // danmaku overlay). Fullscreening the bare <video> loses that chrome
  // because it's normally rendered as sibling markup, not children of
  // <video>. This is a size heuristic, not a per-site selector: it holds
  // for any site's player skin without naming it.
  function fullscreenTarget(video) {
    var videoRect = video.getBoundingClientRect();
    var videoArea = videoRect.width * videoRect.height;
    var node = video.parentElement;
    var target = video;
    for (var i = 0; i < 6 && node; i++) {
      var rect = node.getBoundingClientRect();
      if (rect.width * rect.height > videoArea * 1.02) target = node;
      node = node.parentElement;
    }
    return target;
  }

  // ── Fullscreen: generic CSS pin, not the Fullscreen API ────────────────
  // The native website window is already a full-bleed screen surface, so
  // "fullscreen" only means: make the player fill the page. requestFullscreen
  // needs transient user activation, which injected script does not have —
  // so the reliable generic path is our own 网页全屏: pin the player container
  // over the viewport with plain CSS, nudge the site's resize handling, and
  // hide fixed chrome that still paints above it. Verified end-to-end on
  // bilibili (video resized to fill; header/login mask hidden; clean restore).

  var pinState = null; // { el, savedStyle, savedHtmlOverflow, savedBodyOverflow, hidden: [{el, visibility}] }

  function viewportFilled(el) {
    if (!el) return false;
    var r = el.getBoundingClientRect();
    return r.width >= innerWidth * 0.9 && r.height >= innerHeight * 0.9;
  }

  function nudgeResize() {
    // Untrusted resize still reaches addEventListener('resize') handlers, so
    // players that size the <video> in px re-run their layout math.
    try { window.dispatchEvent(new Event('resize')); } catch (_) {}
  }

  // Hide fixed/overlay branches that paint above the pinned player (site
  // header, floating chrome, modal masks). elementsFromPoint keeps this exact:
  // only actual occluders are touched, all restored on exit. Zero site
  // knowledge — paint order is the probe, not class names.
  function hideOccluders(target) {
    var pts = [
      [innerWidth / 2, innerHeight / 2],
      [innerWidth / 2, 20],
      [innerWidth / 2, innerHeight - 20],
      [20, innerHeight / 2],
      [innerWidth - 20, innerHeight / 2],
    ];
    var offenders = [];
    for (var i = 0; i < pts.length; i++) {
      var stack;
      try { stack = document.elementsFromPoint(pts[i][0], pts[i][1]); } catch (_) { stack = []; }
      for (var j = 0; j < stack.length; j++) {
        var el = stack[j];
        if (el === target || target.contains(el)) break; // reached the player; rest is behind
        if (el === document.documentElement || el === document.body) continue;
        var root = el;
        while (root.parentElement && !root.parentElement.contains(target)) root = root.parentElement;
        if (root === document.documentElement || root === document.body) continue;
        if (root.contains(target)) continue;
        if (offenders.indexOf(root) === -1) offenders.push(root);
      }
    }
    for (var k = 0; k < offenders.length; k++) {
      pinState.hidden.push({ el: offenders[k], visibility: offenders[k].style.visibility });
      offenders[k].style.visibility = 'hidden';
    }
  }

  function applyPinStyle(el) {
    el.style.position = 'fixed';
    el.style.top = '0';
    el.style.left = '0';
    el.style.right = '0';
    el.style.bottom = '0';
    el.style.width = '100vw';
    el.style.height = '100vh';
    el.style.margin = '0';
    el.style.zIndex = '2147483645';
    el.style.background = '#000';
  }

  function cssPinEnter(video) {
    var target = fullscreenTarget(video);
    var savedHtmlOverflow = document.documentElement.style.overflow;
    var savedBodyOverflow = document.body ? document.body.style.overflow : '';
    document.documentElement.style.overflow = 'hidden';
    if (document.body) document.body.style.overflow = 'hidden';

    function abort() {
      document.documentElement.style.overflow = savedHtmlOverflow;
      if (document.body) document.body.style.overflow = savedBodyOverflow;
      return false;
    }

    function restore(el, savedStyle) {
      try {
        if (savedStyle) el.setAttribute('style', savedStyle);
        else el.removeAttribute('style');
      } catch (_) {}
    }

    // Pin el and require the fill to actually stick. A single true reading
    // is not enough: some players' own resize handling briefly matches our
    // pin, then snap the <video> back with a layout pass moments later —
    // observed on Tencent's TXP player, where the ancestor we pin does
    // expand to fill the viewport, but the <video> itself (sized as 100% of
    // an intermediate position:absolute wrapper) keeps reverting because
    // that percentage resolves against a different, unaffected positioned
    // ancestor. Require the fill to hold a further beat before trusting it.
    function tryPin(el) {
      var savedStyle = el.getAttribute('style') || '';
      applyPinStyle(el);
      nudgeResize();
      return settle(function () { return viewportFilled(video); }, 700).then(function (filled) {
        if (!filled) {
          restore(el, savedStyle);
          return false;
        }
        return settle(function () { return !viewportFilled(video); }, 400).then(function (reverted) {
          if (reverted) {
            restore(el, savedStyle);
            return false;
          }
          pinState = { el: el, savedStyle: savedStyle, savedHtmlOverflow: savedHtmlOverflow, savedBodyOverflow: savedBodyOverflow, hidden: [] };
          hideOccluders(pinState.el);
          return true;
        });
      });
    }

    return tryPin(target).then(function (ok) {
      if (ok) return true;
      if (target === video) return abort();
      // Ancestor pin didn't durably resize the video — pin the <video>
      // itself directly. This loses any chrome rendered as a sibling outside
      // <video> (controls, danmaku), but a working plain video beats a pin
      // that visually never took effect.
      return tryPin(video).then(function (ok2) {
        return ok2 || abort();
      });
    });
  }

  function cssPinExit() {
    if (!pinState) return;
    var state = pinState;
    pinState = null;
    for (var i = 0; i < state.hidden.length; i++) {
      try { state.hidden[i].el.style.visibility = state.hidden[i].visibility; } catch (_) {}
    }
    try {
      if (state.savedStyle) state.el.setAttribute('style', state.savedStyle);
      else state.el.removeAttribute('style');
    } catch (_) {}
    document.documentElement.style.overflow = state.savedHtmlOverflow;
    if (document.body) document.body.style.overflow = state.savedBodyOverflow;
    nudgeResize();
  }

  // Baseline resting size of a <video>, captured once before we ever act on
  // it. Only used to judge the SITE_KEYS toggle path below (bilibili/iqiyi's
  // own "网页全屏", not the native Fullscreen API) — there is no DOM signal
  // for "is the site's own web-fullscreen active", so it can only be
  // inferred from geometry. An absolute viewport-fraction threshold
  // (viewportFilled alone) is not enough: some sites' *normal*, un-toggled
  // layout already fills most of a large native window (observed on iQIYI),
  // which made every "enter" call see a false "already fullscreen" and
  // no-op, while "exit" — believing it needed to leave a state that was
  // never actually entered — dispatched the toggle key and *caused* an
  // unwanted real entry. Requiring growth relative to the video's own
  // resting size (not just an absolute size) tells the two apart.
  var restingSize = null; // { video, w, h }

  function recordResting(video) {
    if (restingSize && restingSize.video === video) return;
    var r = video.getBoundingClientRect();
    restingSize = { video: video, w: r.width, h: r.height };
  }

  function grownFullscreen(video) {
    if (!video || !restingSize || restingSize.video !== video) return false;
    if (!viewportFilled(video)) return false;
    var r = video.getBoundingClientRect();
    return r.width > restingSize.w * 1.2 && r.height > restingSize.h * 1.2;
  }

  // Live, not remembered: the user can exit fullscreen with their own mouse
  // or a real Esc between two phone button presses, so "are we fullscreen"
  // is re-derived fresh on every call from observable signals, never from a
  // flag this script set on a previous call. pinState is the one exception —
  // it names an element *we* put an inline style on, which nothing but our
  // own cssPinExit() touches, so it cannot desync the way a claim about the
  // site's own (externally changeable) fullscreen state could.
  function currentlyFullscreen(video) {
    if (document.fullscreenElement || document.webkitFullscreenElement) return true;
    if (pinState) return true;
    return grownFullscreen(video);
  }

  // WebView2's renderer can put an element into the DOM Fullscreen API, but
  // unlike a full browser it cannot change the embedding Win32 host window.
  // Report the real document state to the Windows shell so a user's own click
  // on a site's fullscreen button gets true monitor fullscreen (no title bar
  // or taskbar), and Esc / the site's exit button restores the normal window.
  // AddScriptToExecuteOnDocumentCreated runs in child frames too, which matters
  // for cross-origin embedded players. Other hosts simply do not expose the
  // optional binding, so this remains harmless in WKWebView and unit tests.
  function reportNativeFullscreen(active) {
    try {
      if (typeof window.tinyplayWebsiteSetFullscreen !== 'function') return;
      var result = window.tinyplayWebsiteSetFullscreen(!!active);
      if (result && typeof result.catch === 'function') result.catch(function () {});
    } catch (_) {}
  }

  function onNativeFullscreenChange() {
    reportNativeFullscreen(!!(document.fullscreenElement || document.webkitFullscreenElement));
  }

  document.addEventListener('fullscreenchange', onNativeFullscreenChange);
  document.addEventListener('webkitfullscreenchange', onNativeFullscreenChange);
  // Navigation normally emits fullscreenchange first. This top-level fallback
  // prevents a disappearing document from ever stranding the HWND fullscreen.
  window.addEventListener('pagehide', function () {
    try {
      if (window.top === window) reportNativeFullscreen(false);
    } catch (_) {}
  });

  function enterFullscreen() {
    var video = primaryVideo();
    if (!video) return failed('no_player');
    recordResting(video);
    if (currentlyFullscreen(video)) {
      // Already there (including via the user's own native double-click) —
      // report success without touching anything, since a site's dblclick
      // handler is commonly itself a toggle and would exit if we double-hit it.
      return { ok: true, status: 'fullscreen_enter' };
    }
    // Enter waterfall: ① verified site web-fullscreen key (keeps the site's
    // own chrome/danmaku behavior) ② generic CSS pin ③ Fullscreen API as a
    // last resort — it needs user activation, so it usually cannot work from
    // injected script, but it is free to try and its oracle is exact.
    var key = siteKey('fullscreen');
    var viaKey = key && dispatchKey(key)
      ? settle(function () { return grownFullscreen(video) || !!document.fullscreenElement; }, 900)
      : Promise.resolve(false);
    return viaKey.then(function (keyed) {
      if (keyed) return { ok: true, status: 'fullscreen_enter' };
      return cssPinEnter(video).then(function (pinned) {
        if (pinned) return { ok: true, status: 'fullscreen_enter' };
        var target = fullscreenTarget(video);
        var req = target.requestFullscreen || target.webkitRequestFullscreen;
        if (req) {
          try {
            var ret = req.call(target);
            if (ret && typeof ret.catch === 'function') ret.catch(function () {});
          } catch (_) {}
          return settle(function () {
            return !!(document.fullscreenElement || document.webkitFullscreenElement);
          }, 500).then(function (native) {
            return native ? { ok: true, status: 'fullscreen_enter' } : failed('effect_unconfirmed');
          });
        }
        return failed('effect_unconfirmed');
      });
    });
  }

  function exitFullscreen() {
    var video = primaryVideo();
    if (!currentlyFullscreen(video)) {
      // Nothing to do — safe to call unconditionally, e.g. right after the
      // user already exited natively with their own mouse/Esc.
      return { ok: true, status: 'fullscreen_exit' };
    }
    if (document.fullscreenElement || document.webkitFullscreenElement) {
      var exit = document.exitFullscreen || document.webkitExitFullscreen;
      if (exit) {
        try {
          exit.call(document);
          return { ok: true, status: 'fullscreen_exit' };
        } catch (_) {}
      }
    }
    if (pinState) {
      cssPinExit();
      return { ok: true, status: 'fullscreen_exit' };
    }
    // Left: a site's own (non-native) web-fullscreen, detected only via
    // grownFullscreen above — exit through the same verified key that enters it.
    var key = siteKey('fullscreen');
    if (key && video && dispatchKey(key)) {
      return settle(function () { return !viewportFilled(video); }, 900).then(function (exited) {
        return exited ? { ok: true, status: 'fullscreen_exit' } : failed('effect_unconfirmed');
      });
    }
    return failed('effect_unconfirmed');
  }

  function doSearch(text) {
    var template = findURLTemplate(SEARCH_URL_TEMPLATES);
    if (template) {
      window.location.assign(template.replace('{q}', encodeURIComponent(text || '')));
      return { ok: true, status: 'search' };
    }
    var input = findSearchInput();
    if (!input) {
      return { ok: false, status: 'error', error: 'no_search' };
    }
    input.focus();
    setNativeValue(input, text || '');
    submitSearch(input);
    return { ok: true, status: 'search' };
  }

  function doType(text) {
    var active = document.activeElement;
    if (active && (active.tagName === 'INPUT' || active.tagName === 'TEXTAREA' || active.isContentEditable)) {
      if (active.isContentEditable) {
        active.textContent = text || '';
        try {
          active.dispatchEvent(new Event('input', { bubbles: true }));
        } catch (_) {}
      } else {
        setNativeValue(active, text || '');
      }
      return { ok: true, status: 'type' };
    }
    var input = findSearchInput();
    if (!input) {
      return { ok: false, status: 'error', error: 'no_input' };
    }
    input.focus();
    setNativeValue(input, text || '');
    return { ok: true, status: 'type' };
  }

  function doEnter() {
    var active = document.activeElement;
    if (active && (active.tagName === 'INPUT' || active.tagName === 'TEXTAREA')) {
      if (submitSearch(active)) {
        return { ok: true, status: 'enter' };
      }
    }
    var input = findSearchInput();
    if (input) {
      submitSearch(input);
      return { ok: true, status: 'enter' };
    }
    var btn = queryDeep(['button[type="submit"]', 'input[type="submit"]']);
    if (btn) {
      btn.click();
      return { ok: true, status: 'enter' };
    }
    return { ok: false, status: 'error', error: 'no_enter_target' };
  }

  function clearHints() {
    if (hintState.root && hintState.root.parentNode) {
      hintState.root.parentNode.removeChild(hintState.root);
    }
    if (hintState.style && hintState.style.parentNode) {
      hintState.style.parentNode.removeChild(hintState.style);
    }
    hintState.root = null;
    hintState.style = null;
    hintState.map = Object.create(null);
    hintState.active = false;
  }

  // Fixed 12-key phone alphabet (matches Go website.HintAlphabet). Every label
  // is exactly two symbols in deterministic order: AA, AX, …, 99 (144 max).
  var HINT_ALPHABET = 'AXY123456789';
  var MAX_HINT_TARGETS = HINT_ALPHABET.length * HINT_ALPHABET.length;
  // Semantic interactive surfaces — collected first.
  var SEMANTIC_HINT_SEL =
    'a[href], button, input, select, textarea, summary, [role="button"], [role="link"], [role="tab"], [role="menuitem"], [onclick], [tabindex]:not([tabindex="-1"])';

  function alphabetLabels(count) {
    var out = [];
    if (count > MAX_HINT_TARGETS) count = MAX_HINT_TARGETS;
    for (var i = 0; i < HINT_ALPHABET.length && out.length < count; i++) {
      for (var j = 0; j < HINT_ALPHABET.length && out.length < count; j++) {
        out.push(HINT_ALPHABET.charAt(i) + HINT_ALPHABET.charAt(j));
      }
    }
    return out;
  }

  function alreadyCovered(seen, el) {
    if (seen.has(el)) return true;
    // Nested inside an already-hinted ancestor: the outer surface owns the label.
    var node = el.parentElement;
    while (node) {
      if (seen.has(node)) return true;
      node = node.parentElement;
    }
    return false;
  }

  function markSeen(seen, el) {
    seen.add(el);
  }

  function isSemanticHintEl(el) {
    if (!el || el.nodeType !== 1) return false;
    if (el.matches) {
      try {
        if (el.matches(SEMANTIC_HINT_SEL)) return true;
      } catch (_) {}
    }
    var tag = el.tagName;
    if (tag === 'A' && el.href) return true;
    if (tag === 'BUTTON' || tag === 'SELECT' || tag === 'TEXTAREA' || tag === 'SUMMARY') return true;
    if (tag === 'INPUT') {
      var t = (el.getAttribute('type') || 'text').toLowerCase();
      return t !== 'hidden' && t !== 'file';
    }
    var role = (el.getAttribute('role') || '').toLowerCase();
    if (role === 'button' || role === 'link' || role === 'tab' || role === 'menuitem') return true;
    if (el.hasAttribute('onclick')) return true;
    var tab = el.getAttribute('tabindex');
    if (tab !== null && tab !== '' && tab !== '-1') return true;
    return false;
  }

  function pushHintTarget(acc, seen, el, frameOffset) {
    if (!el || alreadyCovered(seen, el)) return false;
    if (el.disabled || el.getAttribute('aria-hidden') === 'true') return false;
    if (el.tagName === 'INPUT') {
      var type = (el.getAttribute('type') || 'text').toLowerCase();
      if (type === 'hidden' || type === 'file') return false;
    }
    // Full reachability: ancestor constraints + viewport clip + hit-test.
    if (!isHintReachable(el)) return false;
    markSeen(seen, el);
    acc.push({ el: el, ox: frameOffset.x, oy: frameOffset.y });
    return true;
  }

  // 1) Classic interactive elements (semantic pass).
  function collectSemanticTargets(doc, acc, seen, frameOffset) {
    var nodes;
    try {
      nodes = doc.querySelectorAll(SEMANTIC_HINT_SEL);
    } catch (_) {
      return;
    }
    for (var i = 0; i < nodes.length; i++) {
      pushHintTarget(acc, seen, nodes[i], frameOffset);
    }
  }

  // Repeated, similarly sized sibling boxes are a site-agnostic signal for
  // delegated click rows. Requires a second signal (pointer cursor, identity
  // data, list-ish parent, or li/tr) so ordinary paragraphs do not become Hints.
  function looksLikeRepeatedRow(el, rect) {
    var parent = el.parentElement;
    if (!parent || parent.children.length < 2) return false;
    var matched = 0;
    for (var i = 0; i < parent.children.length && matched < 2; i++) {
      var sibling = parent.children[i];
      if (sibling === el || sibling.tagName !== el.tagName) continue;
      var sr = sibling.getBoundingClientRect();
      if (sr.width < 2 || sr.height < 2) continue;
      if (sr.width >= rect.width * 0.5 && sr.width <= rect.width * 1.8 &&
          sr.height >= rect.height * 0.45 && sr.height <= rect.height * 2.2) {
        matched++;
      }
    }
    return matched > 0;
  }

  function hasIdentityData(el) {
    return el.hasAttribute('data-key') || el.hasAttribute('data-id') ||
      el.hasAttribute('data-index') || el.hasAttribute('data-item-id') ||
      el.hasAttribute('data-value') || el.hasAttribute('data-cid') ||
      el.hasAttribute('data-aid');
  }

  // 2) Conservative site-agnostic heuristic for visible row-like elements that
  // rely on delegated click handlers (no semantic tag/role/tabindex/onclick).
  // Guardrails: reasonable row geometry, non-empty text, no nested competing
  // semantic target, repeated-sibling shape + interaction signal, hit-test gate.
  function collectDelegatedRowTargets(doc, acc, seen, frameOffset) {
    var candidates;
    try {
      candidates = doc.querySelectorAll(
        'li, [role="listitem"], [role="option"], [role="row"], tr, div, article, section'
      );
    } catch (_) {
      return;
    }
    for (var i = 0; i < candidates.length; i++) {
      var el = candidates[i];
      if (alreadyCovered(seen, el)) continue;
      if (isSemanticHintEl(el)) continue;
      // Nested competing semantic control ⇒ semantic pass owns the click surface.
      var nested;
      try {
        nested = el.querySelector(SEMANTIC_HINT_SEL);
      } catch (_) {
        nested = null;
      }
      if (nested && isHintReachable(nested)) continue;
      // Geometry: row-like, not a full-page slab, not a tiny icon chip.
      var vis = visibleClientRect(el);
      if (!vis) continue;
      if (vis.height < 18 || vis.height > 180) continue;
      if (vis.width < 48) continue;
      if (vis.width > (ownerWindow(el).innerWidth || innerWidth) * 0.98 && vis.height > 100) continue;
      // Must carry user-visible text (episode titles, channel names, …).
      var text = (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim();
      if (!text || text.length < 2 || text.length > 220) continue;
      // Avoid tagging pure prose blocks (long paragraphs).
      if (text.length > 80 && vis.height > 72) continue;
      var style;
      try { style = ownerWindow(el).getComputedStyle(el); } catch (_) { style = null; }
      var cursorOk = style && style.cursor === 'pointer';
      var role = (el.getAttribute('role') || '').toLowerCase();
      var roleOk = role === 'listitem' || role === 'option' || role === 'row' || role === 'button' || role === 'link';
      var parent = el.parentElement;
      var parentListish = false;
      if (parent) {
        var pRole = (parent.getAttribute('role') || '').toLowerCase();
        var pTag = parent.tagName;
        var pClass = parent.className ? String(parent.className) : '';
        parentListish = pTag === 'UL' || pTag === 'OL' || pTag === 'TBODY' || pTag === 'TABLE' ||
          pRole === 'list' || pRole === 'listbox' || pRole === 'menu' || pRole === 'grid' ||
          /list|pod|playlist|episode|items|rows/i.test(pClass);
      }
      var repeated = looksLikeRepeatedRow(el, vis);
      var identity = hasIdentityData(el);
      // Require a clear interaction/list signal so ordinary prose is skipped.
      if (!cursorOk && !roleOk && !parentListish && !identity &&
          el.tagName !== 'LI' && el.tagName !== 'TR') {
        continue;
      }
      // Div/article shells without list parent need the repeated-row shape or
      // cursor/identity; plain single blocks stay unlabeled.
      if ((el.tagName === 'DIV' || el.tagName === 'ARTICLE' || el.tagName === 'SECTION') &&
          !parentListish && !cursorOk && !identity && !roleOk) {
        continue;
      }
      if ((el.tagName === 'DIV' || el.tagName === 'ARTICLE' || el.tagName === 'SECTION') &&
          !repeated && !cursorOk && !identity) {
        continue;
      }
      pushHintTarget(acc, seen, el, frameOffset);
    }
  }

  // 3) Additive site adapters for known delegated-click surfaces. They run
  // before the generic row scan so the site-owned outer click target claims
  // its label and the generic pass cannot also label a nested text wrapper.
  // Bilibili playlist rows keep Vue's bubbled click on the outer .pod-item
  // (do not dive into inner title spans).
  function collectSiteAdapterTargets(doc, acc, seen, frameOffset) {
    if (/(^|\.)bilibili\.com$/i.test(HOST)) {
      var rows;
      try {
        // Confirmed Bilibili right-hand playlist row surface.
        rows = doc.querySelectorAll('.video-pod__list .pod-item.video-pod__item');
      } catch (_) {
        rows = [];
      }
      for (var i = 0; i < rows.length; i++) {
        // Outer .pod-item preserves the site's delegated Vue click handling.
        pushHintTarget(acc, seen, rows[i], frameOffset);
      }
    }
    if (/(^|\.)iqiyi\.com$/i.test(HOST)) {
      var episodes;
      try {
        // iQIYI's right-hand episode picker is a delegated-click grid made of
        // plain divs: no button/a/role/tabindex/onclick is present. The CSS
        // module hash changes, but these stable class-name prefixes identify
        // the outer tile that owns the click (rather than its inner span).
        episodes = doc.querySelectorAll(
          '[class*="episodesNew_item__"], [class*="episodes_item__"]'
        );
      } catch (_) {
        episodes = [];
      }
      for (var j = 0; j < episodes.length; j++) {
        pushHintTarget(acc, seen, episodes[j], frameOffset);
      }
    }
  }

  function collectTargets(doc, acc, seen, frameOffset) {
    frameOffset = frameOffset || { x: 0, y: 0 };
    seen = seen || new Set();
    collectSemanticTargets(doc, acc, seen, frameOffset);
    collectSiteAdapterTargets(doc, acc, seen, frameOffset);
    collectDelegatedRowTargets(doc, acc, seen, frameOffset);
    // Same-origin iframes only.
    var iframes;
    try {
      iframes = doc.querySelectorAll('iframe');
    } catch (_) {
      return;
    }
    for (var j = 0; j < iframes.length; j++) {
      var frame = iframes[j];
      if (!isHintReachable(frame) && !isVisible(frame)) continue;
      var childDoc = null;
      try {
        childDoc = frame.contentDocument;
      } catch (_) {
        childDoc = null;
      }
      if (!childDoc) continue;
      var fr = frame.getBoundingClientRect();
      collectTargets(childDoc, acc, seen, {
        x: frameOffset.x + fr.left,
        y: frameOffset.y + fr.top,
      });
    }
  }

  // Collect and render one snapshot. A video site's SPA can replace the
  // right-hand episode panel just as the phone taps "select page element";
  // callers that see no targets may retry this snapshot shortly afterwards.
  function enterHintsOnce() {
    clearHints();
    var targets = [];
    var seen = new Set();
    collectTargets(document, targets, seen, { x: 0, y: 0 });
    // Apply the 144-target cap in visual order, not raw DOM order: a dense
    // header cannot starve a visible playlist simply because it occurs first.
    targets.sort(function (a, b) {
      var ar = visibleClientRect(a.el) || a.el.getBoundingClientRect();
      var br = visibleClientRect(b.el) || b.el.getBoundingClientRect();
      var ap = labelPoint(ar);
      var bp = labelPoint(br);
      var ay = a.oy + ap.y;
      var by = b.oy + bp.y;
      if (Math.abs(ay - by) > 2) return ay - by;
      return (a.ox + ap.x) - (b.ox + bp.x);
    });
    // Cap at 12×12 two-symbol labels (phone keypad alphabet).
    if (targets.length > MAX_HINT_TARGETS) targets = targets.slice(0, MAX_HINT_TARGETS);
    if (!targets.length) {
      return { ok: false, status: 'error', error: 'no_targets', hint_active: false };
    }
    var labels = alphabetLabels(targets.length);
    var style = document.createElement('style');
    style.id = 'tinyplay-hint-style';
    style.textContent =
      '#tinyplay-hint-root{position:fixed;inset:0;z-index:2147483646;pointer-events:none;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}' +
      '.tinyplay-hint-label{position:absolute;transform:translate(-50%,-50%);min-width:18px;padding:2px 5px;border-radius:4px;' +
      'background:#ffd60a;color:#111;font-size:12px;font-weight:800;line-height:1.2;box-shadow:0 1px 4px rgba(0,0,0,.45);' +
      'border:1px solid rgba(0,0,0,.25);letter-spacing:.04em}';
    document.documentElement.appendChild(style);
    var root = document.createElement('div');
    root.id = 'tinyplay-hint-root';
    for (var i = 0; i < targets.length; i++) {
      var t = targets[i];
      var label = labels[i];
      hintState.map[label] = t.el;
      var vis = visibleClientRect(t.el) || t.el.getBoundingClientRect();
      var pt = labelPoint(vis);
      var marker = document.createElement('div');
      marker.className = 'tinyplay-hint-label';
      marker.textContent = label;
      // frameOffset shifts iframe-local coords into the top window.
      marker.style.left = Math.round(t.ox + pt.x) + 'px';
      marker.style.top = Math.round(t.oy + pt.y) + 'px';
      root.appendChild(marker);
    }
    document.documentElement.appendChild(root);
    hintState.root = root;
    hintState.style = style;
    hintState.active = true;
    // Return exactly the labels mounted on the TV. The phone uses this list to
    // disable keypad choices that cannot select a visible target.
    return { ok: true, status: 'hint_enter', hint_active: true, labels: labels };
  }

  function enterHints() {
    var attempts = 0;
    function tryEnter() {
      var result = enterHintsOnce();
      // Only retry the transient empty-DOM case. Any other result is either a
      // successful overlay or a real error that should be reported as-is.
      if (result.ok || result.error !== 'no_targets' || attempts >= 2) return result;
      attempts++;
      return new Promise(function (resolve) {
        window.setTimeout(function () { resolve(tryEnter()); }, 350);
      });
    }
    return tryEnter();
  }

  // Pick the centre of the actually visible portion, rather than the label's
  // left-edge placement. This puts a playlist activation in the row's real
  // content even when it is horizontally/vertically clipped by a scroller.
  function hintClickPoint(el) {
    var visible = visibleClientRect(el);
    if (!visible) return null;
    var doc = el.ownerDocument || document;
    var x = visible.left + visible.width / 2;
    var y = visible.top + visible.height / 2;
    var hit = null;
    try { hit = doc.elementFromPoint(x, y); } catch (_) { hit = null; }
    if (!hit || (hit !== el && !el.contains(hit))) return null;
    return { x: x, y: y, target: hit };
  }

  function dispatchHintPointerClick(el) {
    var point = hintClickPoint(el);
    if (!point) return false;
    var target = point.target;
    var view = ownerWindow(target);
    var common = {
      bubbles: true,
      cancelable: true,
      composed: true,
      view: view,
      clientX: point.x,
      clientY: point.y,
      screenX: (view.screenX || 0) + point.x,
      screenY: (view.screenY || 0) + point.y,
      button: 0,
      detail: 1,
    };
    try {
      // Pointer handlers are common on modern player UIs; MouseEvent remains
      // necessary for older handlers and for Vue's normal @click path.
      if (typeof view.PointerEvent === 'function') {
        target.dispatchEvent(new view.PointerEvent('pointerdown', Object.assign({}, common, {
          pointerId: 1, pointerType: 'mouse', isPrimary: true, buttons: 1,
        })));
      }
      target.dispatchEvent(new view.MouseEvent('mousedown', Object.assign({}, common, { buttons: 1 })));
      if (typeof view.PointerEvent === 'function') {
        target.dispatchEvent(new view.PointerEvent('pointerup', Object.assign({}, common, {
          pointerId: 1, pointerType: 'mouse', isPrimary: true, buttons: 0,
        })));
      }
      target.dispatchEvent(new view.MouseEvent('mouseup', Object.assign({}, common, { buttons: 0 })));
      target.dispatchEvent(new view.MouseEvent('click', Object.assign({}, common, { buttons: 0 })));
      try { target.focus({ preventScroll: true }); } catch (_) {}
      return true;
    } catch (_) {
      return false;
    }
  }

  function selectHint(label) {
    if (!hintState.active) {
      return { ok: false, status: 'error', error: 'hint_inactive', hint_active: false };
    }
    var key = String(label || '').trim().toUpperCase();
    var el = hintState.map[key];
    clearHints();
    if (!el) {
      return { ok: false, status: 'error', error: 'unknown_label', hint_active: false };
    }
    try {
      // Video-site result links commonly target a new tab, which an embedded
      // WebView may discard. Keep Hint navigation in this dedicated TV window.
      if (el.tagName === 'A' && el.href) {
        window.location.assign(el.href);
        return { ok: true, status: 'hint_label', hint_active: false };
      }
    } catch (_) {}
    // Calling .click() on a layout wrapper makes the event target the wrapper.
    // Delegated Vue handlers often live on a child row, so resolve a real hit
    // at the visible centre and replay the normal pointer/mouse sequence there.
    // JavaScript cannot create a trusted event; this is deliberately a safe
    // browser-level fallback, not a claim to emulate physical input.
    if (dispatchHintPointerClick(el)) {
      return { ok: true, status: 'hint_label', hint_active: false };
    }
    try {
      el.focus({ preventScroll: false });
    } catch (_) {
      try { el.focus(); } catch (__) {}
    }
    try { el.click(); } catch (_) {}
    return { ok: true, status: 'hint_label', hint_active: false };
  }

  function exitHints() {
    clearHints();
    return { ok: true, status: 'hint_exit', hint_active: false };
  }

  // Clean overlays on navigation.
  window.addEventListener('pagehide', clearHints, true);
  window.addEventListener('beforeunload', clearHints, true);

  // ── Single-container guard ──────────────────────────────────────────────
  // The desktop website window is one lone WebView2 with no tab bar and no
  // popup surface, and WebView2's default new-window behaviour spawns a second
  // top-level window that our injected controller never reaches and the close
  // button never tears down. So every route to a second window — window.open,
  // target="_blank"/_new links and forms, <base target> — is folded back into
  // this same page. There must only ever be one container.
  function navigateHere(url) {
    if (!url) return;
    try {
      window.location.assign(String(url));
    } catch (_) {
      try { window.location.href = String(url); } catch (__) {}
    }
  }

  // Minimal window-like stub so the common login-popup pattern
  // (`var w = window.open(); w.location = url;`) still lands in this page
  // instead of throwing on a null return.
  function stubWindow() {
    var loc = {
      assign: navigateHere,
      replace: navigateHere,
      reload: function () { try { window.location.reload(); } catch (_) {} },
      get href() { return ''; },
      set href(v) { navigateHere(v); },
    };
    return {
      closed: false,
      focus: function () {},
      blur: function () {},
      close: function () {},
      postMessage: function () {},
      get location() { return loc; },
      set location(v) { navigateHere(typeof v === 'string' ? v : (v && v.href)); },
    };
  }

  try {
    window.open = function (url) {
      navigateHere(url);
      return stubWindow();
    };
  } catch (_) {}

  function effectiveTarget(anchor) {
    var t = (anchor.getAttribute && anchor.getAttribute('target')) || '';
    if (!t) {
      var base = document.querySelector('base[target]');
      if (base) t = base.getAttribute('target') || '';
    }
    return t.toLowerCase();
  }

  // Anchors: keep every _blank / _new navigation in this window.
  document.addEventListener('click', function (event) {
    var target = event.target;
    var link = target && target.closest ? target.closest('a[href]') : null;
    if (!link || !link.href) return;
    var t = effectiveTarget(link);
    if (t !== '_blank' && t !== '_new') return;
    event.preventDefault();
    navigateHere(link.href);
  }, true);

  // Forms: rewrite a _blank target at submit time so the result renders in-page.
  document.addEventListener('submit', function (event) {
    var form = event.target;
    if (!form || form.tagName !== 'FORM') return;
    var t = (form.getAttribute('target') || '').toLowerCase();
    if (t === '_blank' || t === '_new') form.setAttribute('target', '_self');
  }, true);
  try {
    var mo = new MutationObserver(function () {
      // If the SPA replaced the body, drop stale overlays.
      if (hintState.active && hintState.root && !document.documentElement.contains(hintState.root)) {
        clearHints();
      }
      // Pinned player got swapped out by the SPA: the saved styles died with
      // the old element, so just drop the pin and unfreeze scrolling.
      if (pinState && !document.documentElement.contains(pinState.el)) {
        var stale = pinState;
        pinState = null;
        for (var i = 0; i < stale.hidden.length; i++) {
          try { stale.hidden[i].el.style.visibility = stale.hidden[i].visibility; } catch (_) {}
        }
        document.documentElement.style.overflow = stale.savedHtmlOverflow;
        if (document.body) document.body.style.overflow = stale.savedBodyOverflow;
      }
    });
    mo.observe(document.documentElement, { childList: true, subtree: true });
  } catch (_) {}

  // handle() returns either a plain result object (sync actions) or a Promise
  // of one (oracle-checked transport actions). Shells resolve both shapes.
  // Promises always resolve — waterfall failures are result objects, and
  // rejections are normalized here so no shell sees an unhandled rejection.
  function handle(cmd) {
    cmd = cmd || {};
    var action = String(cmd.action || '');
    var result;
    try {
      result = dispatchAction(action, cmd);
    } catch (err) {
      return failed('exception');
    }
    if (result && typeof result.then === 'function') {
      return result.then(
        function (r) { return r || failed('exception'); },
        function () { return failed('exception'); }
      );
    }
    return result;
  }

  function dispatchAction(action, cmd) {
    switch (action) {
      case 'play_pause':
        return togglePlayPause();
      case 'fullscreen_enter':
        return enterFullscreen();
      case 'fullscreen_exit':
        return exitFullscreen();
      case 'seek':
        return seekBy(cmd.text || '');
      case 'speed':
        return setSpeed(cmd.text || '');
      case 'scroll_up':
        return scrollPage('up');
      case 'scroll_down':
        return scrollPage('down');
      case 'login':
        return doLogin();
      case 'search':
        return doSearch(cmd.text || '');
      case 'type':
        return doType(cmd.text || '');
      case 'enter':
        return doEnter();
      case 'hint_enter':
        return enterHints();
      case 'hint_exit':
        return exitHints();
      case 'hint_label':
        return selectHint(cmd.label || '');
      default:
        return failed('unknown_action');
    }
  }

  window.__tinyplayWebsite = {
    __version: 12,
    handle: handle,
    clearHints: clearHints,
    isHintActive: function () { return !!hintState.active; },
  };
})();
