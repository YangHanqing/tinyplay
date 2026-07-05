/* ── State ────────────────────────────────────────────────────────────────── */
let currentSeriesId = '';
let currentSeasonId = '';
let currentSeriesTitle = '';
let currentEpisodeLabel = '';
let currentPosterItemId = '';
let currentItemId = '';   // episode/movie currently playing
let currentItemIsSeries = false;
const tr = (key, params) => window.I18N ? window.I18N.t(key, params) : key;
const trZh = () => !window.I18N || window.I18N.isZh();

// Cached, ascending episode list used by the previous/next buttons so they can
// play the actual adjacent episode (mpv only ever has a single file loaded).
let _navEpisodes = [];
let _navContext = '';   // seasonId|seriesId the cached list belongs to

let browseParentId = '';   // current library filter
let browseStart = 0;
const PAGE_SIZE = 60;
let browseHasMore = false;
let libraryItems = [{ Id: '', Name: tr('all'), CollectionType: '' }];
let currentLibraryName = tr('all');
let libraryLoadError = '';
let libraryLoadSeq = 0;
let browseLoadSeq = 0;

let _homeLibraries = [];  // libraries with type info
let _viewMode = 'home';   // 'home' | 'browse' | 'search'

let isSearching = false;
let searchInFlight = false;
let searchRequestSeq = 0;
let _searchFilter = 'all';   // 'all' | 'Movie' | 'Series' | 'BoxSet'
let _lastSearchItems = [];
let _lastSearchQuery = '';

const EPISODE_PAGE_SIZE = 50;
let sheetSeriesId = '';
let _locateEpisode = null;  // { id, num } to scroll to when opening a series sheet
let sheetEpisodesTotal = 0;
let sheetEpisodesLoading = false;
let _sheetEpisodePage = 0;
let _sheetEpisodeSort = 'asc';

let _propPollTimer = null;
let _propPolling = false;
let _activeTab = 'library';
let _currentPosition = null;
let _currentDuration = null;
let _isDraggingProgress = false;
let _progressPointerId = null;
let _pendingSeekTarget = null; // { seconds, percent, expiry } – suppress poll resets after a seek
let _latestProps = {};
let _hadProps = false;  // true once we've received at least one non-empty prop update
let _playbackInfoOpen = false;
let _subTracksOpen = false;
let _audioTracksOpen = false;
let _speedSheetOpen = false;
let _moreSheetOpen = false;
let _aspectSheetOpen = false;
let _currentAspect = 'fit';
let _loopFile = false;
let _settings = { mpv_cache_secs: 300, seek_backward_secs: 5, seek_forward_secs: 30 };
let _serviceOnline = true;
let _serviceProbeInFlight = false;

// Active source type drives whether the library tab shows the poster wall
// (emby/jellyfin/plex) or the file browser (file). Set by refreshServerSwitcher.
let _activeSourceType = 'emby';
let _filesPath = '';            // current folder path in file mode
let _serverFormType = 'emby';   // type chosen in the add/edit form

const ASPECT_OPTIONS = [
  { value: 'fit',      labelKey: 'aspectFitLabel',      displayKey: 'aspectFitDisplay' },
  { value: 'zoom',     labelKey: 'aspectZoomLabel',     displayKey: 'aspectZoomDisplay' },
  { value: 'stretch',  labelKey: 'aspectStretchLabel',  displayKey: 'aspectStretchDisplay' },
  { value: 'original', labelKey: 'aspectOriginalLabel', displayKey: 'aspectOriginalDisplay' },
];

function aspectLabel(opt) { return tr(opt.labelKey); }
function aspectDisplay(opt) { return tr(opt.displayKey); }

/* ── Boot ─────────────────────────────────────────────────────────────────── */
document.addEventListener('DOMContentLoaded', async () => {
  document.addEventListener('click', onDocClick);
  registerPWA();
  setupServiceReachability();

  await refreshServerSwitcher();
  await restorePlayerContext();
  _setViewMode('home');
  startPropPolling();
  _fetchAndUpdateProps();
  await loadActiveSource();
  await loadSettings();
  fetchEngine();
  fetchSystemVolume();
});

function registerPWA() {
  if (!('serviceWorker' in navigator)) return;
  navigator.serviceWorker.register('/sw.js').catch(() => {});
}

function setupServiceReachability() {
  renderServiceOfflineBanner();
  window.addEventListener('online', () => retryServiceConnection());
  window.addEventListener('offline', () => setServiceOnline(false));
  if (navigator.onLine === false) setServiceOnline(false);
}

function setServiceOnline(online) {
  if (_serviceOnline === online && document.getElementById('service-offline-banner')) return;
  _serviceOnline = online;
  renderServiceOfflineBanner();
}

function renderServiceOfflineBanner() {
  let el = document.getElementById('service-offline-banner');
  if (!el) {
    el = document.createElement('div');
    el.id = 'service-offline-banner';
    el.className = 'service-offline-banner hidden';
    el.setAttribute('role', 'status');
    el.setAttribute('aria-live', 'polite');
    document.body.appendChild(el);
  }
  el.classList.toggle('hidden', _serviceOnline);
  el.innerHTML = `
    <div class="service-offline-icon">!</div>
    <div class="service-offline-copy">
      <div class="service-offline-title">${esc(tr('serviceOfflineTitle'))}</div>
      <div class="service-offline-body">${esc(tr('serviceOfflineBody'))}</div>
    </div>
    <button class="service-offline-action" type="button" onclick="retryServiceConnection()">${esc(tr('serviceOfflineRetry'))}</button>`;
}

async function retryServiceConnection() {
  if (_serviceProbeInFlight) return;
  _serviceProbeInFlight = true;
  try {
    const r = await fetch('/api/player/state', { cache: 'no-store' });
    if (r.ok) {
      setServiceOnline(true);
      toast(tr('serviceOnlineAgain'));
      restorePlayerContext().catch(() => {});
      _fetchAndUpdateProps().catch(() => {});
      refreshServerSwitcher().catch(() => {});
    } else {
      setServiceOnline(false);
    }
  } catch (_) {
    setServiceOnline(false);
  } finally {
    _serviceProbeInFlight = false;
  }
}

function offlineServiceError() {
  const err = new Error(tr('serviceOfflineShort'));
  err.serviceOffline = true;
  return err;
}

/* Load the active source into the library tab: poster wall for media servers,
 * file browser for file sources. */
async function loadActiveSource() {
  if (_activeSourceType === 'file') {
    _enterFileMode();
    await loadDir('');
    return;
  }
  _exitFileMode();
  _setViewMode('home');
  await loadLibraryNav();
  await loadHomeData();
}

/* ── Restore playback context after browser reconnect ───────────────────────
 *
 * When the user closes the browser while mpv is playing, the backend
 * PlayerService._play_context keeps the series_id/season_id in memory.
 * On the next page load we ask the backend for this context and restore
 * the now-playing label.
 */
async function restorePlayerContext() {
  try {
    const state = await api('GET', '/api/player/state');
    if (state.item_id || state.title) {
      currentSeriesId  = state.series_id || '';
      currentSeasonId  = state.season_id || '';
      currentSeriesTitle = state.series_title || '';
      currentEpisodeLabel = state.episode_label || '';
      currentPosterItemId = state.poster_item_id || state.series_id || state.item_id || '';
      currentItemId    = state.item_id || '';
      currentItemIsSeries = !!currentSeriesId;
      setNowPlaying(state.title, {
        seriesTitle: currentSeriesTitle,
        episodeLabel: currentEpisodeLabel,
        posterItemId: currentPosterItemId,
      });
    }
  } catch (_) { /* backend may not be ready yet */ }
}

/* ── API helper ───────────────────────────────────────────────────────────── */
async function api(method, path, body) {
  const opts = { method, headers: { 'Content-Type': 'application/json' } };
  if (body !== undefined) opts.body = JSON.stringify(body);
  let r;
  try {
    r = await fetch(path, opts);
  } catch (_) {
    setServiceOnline(false);
    throw offlineServiceError();
  }
  if (r.status === 503 && r.headers.get('X-TVRemote-Offline') === '1') {
    setServiceOnline(false);
    throw offlineServiceError();
  }
  setServiceOnline(true);
  if (!r.ok) {
    const err = await r.json().catch(() => ({ detail: r.statusText }));
    throw new Error(err.detail || r.statusText);
  }
  return r.json().catch(() => ({}));
}

/* ── Player ───────────────────────────────────────────────────────────────── */
async function cmd(command) {
  try { await api('POST', '/api/player/command', { command }); }
  catch (e) { toast(e.message, true); }
  setTimeout(_fetchAndUpdateProps, 120);
}

function setSpeed(speed) {
  cmd(['set_property', 'speed', speed]);
}

// The volume slider controls the OS output level, not mpv's own gain (mpv
// is pinned at unity gain by the backend) — see docs/CLAUDE.md.
let _systemVolume = { volume: 80, muted: false };

async function fetchSystemVolume() {
  try {
    const d = await api('GET', '/api/system/volume');
    _systemVolume.volume = Number(d.volume);
    _systemVolume.muted = Boolean(d.muted);
    _renderSystemVolumeUi();
  } catch (_) {}
}

async function pushSystemVolume(body) {
  try {
    const d = await api('POST', '/api/system/volume', body);
    if (d.volume != null) _systemVolume.volume = Number(d.volume);
    if (d.muted != null) _systemVolume.muted = Boolean(d.muted);
    _renderSystemVolumeUi();
  } catch (e) {
    toast(e.message, true);
  }
}

function _renderSystemVolumeUi() {
  const safeVol = Math.max(0, Math.min(100, Math.round(_systemVolume.volume)));
  _setText('volume-current', _systemVolume.muted ? tr('muted') : `${safeVol}%`);
  updateVolumeUi(safeVol);
  const muteBtn = document.getElementById('btn-mute');
  if (muteBtn) {
    muteBtn.classList.toggle('active', _systemVolume.muted);
    muteBtn.setAttribute('aria-label', _systemVolume.muted ? tr('unmute') : tr('muted'));
  }
}

function setVolumeByDelta(delta) {
  const base = Number.isFinite(_systemVolume.volume) ? _systemVolume.volume : 80;
  const next = Math.max(0, Math.min(100, Math.round(base + delta)));
  pushSystemVolume({ volume: next });
}

function setVolume(value) {
  const next = Math.max(0, Math.min(100, Math.round(Number(value) || 0)));
  pushSystemVolume({ volume: next });
}

function onVolumePointer(event) {
  if (event.type === 'pointermove' && event.buttons === 0) return;
  const track = document.querySelector('.volume-track');
  if (!track) return;
  const rect = track.getBoundingClientRect();
  if (!rect.width) return;
  const pct = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width));
  updateVolumeUi(pct * 100);
  setVolume(pct * 100);
  event.preventDefault();
}

function toggleMute() {
  pushSystemVolume({ muted: !_systemVolume.muted });
}

function openCurrentSeriesSheet() {
  const targetId = currentSeriesId || currentItemId;
  if (targetId) openVideoSheet(targetId);
}

async function stopPlayer() {
  _hadProps = false;
  document.getElementById('player-info-placeholder')?.classList.add('hidden');
  try {
    await api('POST', '/api/player/stop');
    setNowPlaying('');
    currentItemId = '';
    currentEpisodeLabel = '';
    currentPosterItemId = '';
    currentItemIsSeries = false;
    _loopFile = false;
    _currentAspect = 'fit';
    resetPosterColor();
    loadResume().catch(() => {});
  } catch (e) { toast(e.message, true); }
}

async function stopAndGoBack() {
  await stopPlayer();
  switchTab('library');
}

function openExitConfirm() {
  document.getElementById('exit-confirm-overlay')?.classList.remove('hidden');
}
function closeExitConfirm() {
  document.getElementById('exit-confirm-overlay')?.classList.add('hidden');
}
function confirmExit() {
  closeExitConfirm();
  stopAndGoBack();
}

/* ── Skeleton helpers ─────────────────────────────────────────────────────── */
function skeletonPosterCardHtml() {
  return `
    <div class="skeleton-poster-card">
      <div class="skeleton-poster-frame skeleton-shine"></div>
      <div class="skeleton-poster-info">
        <div class="skeleton-line skeleton-shine" style="width:85%;height:11px"></div>
        <div class="skeleton-line skeleton-shine" style="width:62%;height:11px"></div>
        <div class="skeleton-line skeleton-shine" style="width:38%;height:9px"></div>
      </div>
    </div>`;
}

function skeletonResumeCardHtml() {
  return `<div class="skeleton-resume-card skeleton-shine"></div>`;
}

/* ── Dynamic poster color ─────────────────────────────────────────────────── */
let _posterColorTimer = null;

function applyPosterColor(imgSrc) {
  if (!imgSrc) { resetPosterColor(); return; }
  if (_posterColorTimer) clearTimeout(_posterColorTimer);
  _posterColorTimer = setTimeout(() => {
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = () => {
      try {
        const canvas = document.createElement('canvas');
        const SIZE = 12; canvas.width = SIZE; canvas.height = SIZE;
        const ctx = canvas.getContext('2d');
        ctx.drawImage(img, 0, 0, SIZE, SIZE);
        const data = ctx.getImageData(0, 0, SIZE, SIZE).data;
        let r = 0, g = 0, b = 0;
        const n = SIZE * SIZE;
        for (let i = 0; i < data.length; i += 4) { r += data[i]; g += data[i+1]; b += data[i+2]; }
        r = Math.round(r / n); g = Math.round(g / n); b = Math.round(b / n);
        const tab = document.getElementById('tab-remote');
        if (!tab) return;
        const d = (c) => Math.round(c * 0.13);
        tab.style.background = `
          radial-gradient(circle at 30% 20%, rgba(${r},${g},${b},.32), transparent 52%),
          radial-gradient(circle at 72% 78%, rgba(${r},${g},${b},.18), transparent 48%),
          linear-gradient(180deg, rgb(${d(r)},${d(g)},${d(b)}) 0%, #070c14 100%)`;
      } catch (_) {}
    };
    img.src = imgSrc;
  }, 150);
}

function resetPosterColor() {
  const tab = document.getElementById('tab-remote');
  if (tab) tab.style.background = '';
}

async function playItem(itemId, seriesId, seasonId, title, seriesTitle = '', episodeLabel = '', posterItemId = '') {
  const resolvedPoster = posterItemId || seriesId || itemId || '';
  _hadProps = false;
  _currentAspect = 'fit';
  _loopFile = false;
  currentItemId = itemId;
  currentSeriesId = seriesId || '';
  currentSeasonId = seasonId || '';
  currentSeriesTitle = seriesTitle || '';
  currentEpisodeLabel = episodeLabel || '';
  currentPosterItemId = resolvedPoster;
  currentItemIsSeries = !!currentSeriesId;
  setNowPlaying(title, {
    seriesTitle: currentSeriesTitle,
    episodeLabel: currentEpisodeLabel,
    posterItemId: currentPosterItemId,
  });
  closeVideoSheet();
  document.querySelectorAll('.sheet-episode').forEach(el => {
    el.classList.toggle('playing', el.dataset.itemId === itemId);
  });
  switchTab('remote');
  document.getElementById('play-loading-tag')?.classList.remove('hidden');
  document.getElementById('player-info-placeholder')?.classList.remove('hidden');
  document.getElementById('player-info')?.classList.add('hidden');
  try {
    await api('POST', '/api/player/play', {
      item_id: itemId,
      series_id: currentSeriesId,
      season_id: currentSeasonId,
      title: title || '',
      series_title: currentSeriesTitle,
      episode_label: currentEpisodeLabel,
      poster_item_id: resolvedPoster,
      preferred_language: window.I18N?.lang || navigator.language || 'zh-CN',
    });
    _fetchAndUpdateProps();
  } catch (e) {
    toast(e.message, true);
    setNowPlaying('');
    currentItemId = '';
  } finally {
    document.getElementById('play-loading-tag')?.classList.add('hidden');
  }
  // Preload the season's episode list so previous/next respond instantly.
  if (currentSeriesId) _ensureEpisodeNav(itemId, currentSeriesId, currentSeasonId);
}

/* ── Episode navigation (previous / next) ───────────────────────────────── */
async function _ensureEpisodeNav(itemId, seriesId, seasonId) {
  if (!seriesId) { _navEpisodes = []; _navContext = ''; return; }
  const ctx = seasonId || seriesId;
  if (_navContext === ctx && _navEpisodes.some(e => e.Id === itemId)) return;
  try {
    const qs = new URLSearchParams({
      series_id: seriesId,
      season_id: seasonId || '',
      start: 0,
      limit: 200,
      sort: 'asc',
    });
    const data = await api('GET', `/api/emby/episodes?${qs}`);
    _navEpisodes = data.Items || [];
    _navContext = ctx;
  } catch (_) {
    _navEpisodes = [];
    _navContext = '';
  }
}

function playPrevEpisode() { _stepEpisode(-1); }
function playNextEpisode() { _stepEpisode(1); }

async function _stepEpisode(delta) {
  if (!currentItemIsSeries || !currentItemId) return;
  if (!_navEpisodes.some(e => e.Id === currentItemId)) {
    await _ensureEpisodeNav(currentItemId, currentSeriesId, currentSeasonId);
  }
  const idx = _navEpisodes.findIndex(e => e.Id === currentItemId);
  if (idx < 0) { toast(tr('unableEpisodes'), true); return; }
  const target = _navEpisodes[idx + delta];
  if (!target) { toast(delta > 0 ? tr('alreadyLastEpisode') : tr('alreadyFirstEpisode')); return; }
  playItem(
    target.Id,
    target.SeriesId || currentSeriesId,
    target.SeasonId || currentSeasonId,
    target.Name || '',
    target.SeriesName || currentSeriesTitle || '',
    episodeLabelForItem(target),
    target.SeriesId || currentSeriesId || target.Id,
  );
}

function setNowPlaying(title, meta = {}) {
  const titleEl = document.getElementById('remote-title');
  if (titleEl) titleEl.textContent = title || '';
  _setText('remote-series-title', meta.seriesTitle || '');
  _setText('remote-episode-label', meta.episodeLabel || '');
  document.getElementById('remote-series-title')?.classList.toggle('hidden', !meta.seriesTitle);
  document.getElementById('remote-episode-label')?.classList.toggle('hidden', !meta.episodeLabel);
  const posterItemId = meta.posterItemId || '';
  const nowPlaying = document.getElementById('now-playing');
  const posterImg = document.getElementById('remote-poster-img');
  const posterCard = document.getElementById('remote-poster-card');
  if (nowPlaying) {
    if (posterItemId) {
      // Use Backdrop for blurred bg (server falls back to Primary if unavailable)
      nowPlaying.style.setProperty('--now-poster', `url("/api/emby/image/${encodeURIComponent(posterItemId)}?max_height=700&type=Backdrop")`);
      nowPlaying.classList.add('has-poster');
      if (posterImg) {
        posterImg.src = `/api/emby/image/${encodeURIComponent(posterItemId)}?max_height=520`;
        posterImg.alt = title || '';
      }
      posterCard?.classList.remove('missing');
    } else {
      nowPlaying.style.removeProperty('--now-poster');
      nowPlaying.classList.remove('has-poster');
      if (posterImg) posterImg.removeAttribute('src');
      posterCard?.classList.add('missing');
    }
  }
  const hasTitle = !!title;
  nowPlaying?.classList.toggle('hidden', !hasTitle);
  document.getElementById('remote-empty')?.classList.toggle('hidden', hasTitle);
  document.getElementById('remote-controls')?.classList.toggle('hidden', !hasTitle);
  if (!hasTitle) {
    document.getElementById('player-info')?.classList.add('hidden');
  }
  updateEpisodeNavVisibility(currentItemIsSeries);
  if (posterItemId && hasTitle) {
    applyPosterColor(`/api/emby/image/${encodeURIComponent(posterItemId)}?max_height=80`);
  } else if (!hasTitle) {
    resetPosterColor();
  }
}

function updateEpisodeNavVisibility(isSeries) {
  document.getElementById('ep-controls')?.classList.toggle('hidden', !isSeries);
  const movieInfo = document.getElementById('now-playing-movie-info');
  if (movieInfo) {
    movieInfo.classList.toggle('hidden', isSeries);
    if (!isSeries && !movieInfo.textContent) movieInfo.textContent = tr('movie');
  }
}

/* ── View mode ────────────────────────────────────────────────────────────── */
function _setViewMode(mode) {
  _viewMode = mode;
  document.getElementById('lib-home-view')?.classList.toggle('hidden', mode !== 'home');
  document.getElementById('lib-grid-view')?.classList.toggle('hidden', mode !== 'browse');
  document.getElementById('lib-search-view')?.classList.toggle('hidden', mode !== 'search');
  document.getElementById('lib-search-area')?.classList.toggle('hidden', mode !== 'search');
  document.getElementById('library-toolbar')?.classList.toggle('hidden', mode !== 'browse');
  document.getElementById('lib-files-view')?.classList.toggle('hidden', mode !== 'files');
}

/* ── Tab navigation ───────────────────────────────────────────────────────── */
function switchTab(tab) {
  if (_activeTab === tab) return;
  _activeTab = tab;
  document.getElementById('tab-library').classList.toggle('active', tab === 'library');
  document.getElementById('tab-remote').classList.toggle('active', tab === 'remote');
  document.getElementById('tab-settings').classList.toggle('active', tab === 'settings');
  document.getElementById('nav-library').classList.toggle('active', tab === 'library');
  document.getElementById('nav-remote').classList.toggle('active', tab === 'remote');
  document.getElementById('nav-settings').classList.toggle('active', tab === 'settings');
  document.getElementById('search-toggle')?.classList.toggle('hidden', tab !== 'library' || _activeSourceType === 'file');
  document.getElementById('btn-exit')?.classList.toggle('hidden', tab !== 'remote');
  if (tab !== 'library' && isSearching) cancelSearch();
  if (tab === 'settings') renderSettingsUi();
  if (tab === 'remote') fetchSystemVolume();
}

/* ── Playback info polling ────────────────────────────────────────────────── */
function startPropPolling() {
  if (_propPolling) return;
  _propPolling = true;
  _schedulePropPoll();
}

function stopPropPolling() {
  _propPolling = false;
  clearTimeout(_propPollTimer);
}

let _sysVolPollTicks = 0;

function _schedulePropPoll() {
  if (!_propPolling) return;
  _propPollTimer = setTimeout(async () => {
    if (!document.hidden) {
      await _fetchAndUpdateProps();
      // System volume can change outside the app (physical keys, another
      // app); refresh it every ~5s while the remote tab is open, not every
      // tick — each check spawns a native OS call.
      if (_activeTab === 'remote' && ++_sysVolPollTicks % 5 === 0) fetchSystemVolume();
    }
    _schedulePropPoll();
  }, 1000);
}

async function _fetchAndUpdateProps() {
  try {
    const props = await api('GET', '/api/player/props');
    _applyProps(props);
  } catch (_) {}
}

function _applyProps(p) {
  _latestProps = p || {};
  const hasData = p['time-pos'] != null || p['pause'] != null;

  // Detect unexpected mpv exit (had data before, now empty, still had current item)
  if (!hasData && _hadProps && currentItemId) {
    _hadProps = false;
    const _id = currentItemId;
    currentItemId = '';
    currentItemIsSeries = false;
    _currentAspect = 'fit';
    _loopFile = false;
    setNowPlaying('');
    resetPosterColor();
    api('POST', '/api/player/stop').catch(() => {});
    loadResume().catch(() => {});
    document.getElementById('player-info')?.classList.add('hidden');
    document.getElementById('player-info-placeholder')?.classList.add('hidden');
    document.getElementById('nav-remote-dot')?.classList.add('hidden');
    return;
  }

  // Sync playing state visibility
  document.getElementById('player-info')?.classList.toggle('hidden', !hasData);
  document.getElementById('nav-remote-dot')?.classList.toggle('hidden', !hasData);
  if (hasData) {
    _hadProps = true;
    document.getElementById('now-playing')?.classList.remove('hidden');
    document.getElementById('remote-empty')?.classList.add('hidden');
    document.getElementById('remote-controls')?.classList.remove('hidden');
    document.getElementById('player-info-placeholder')?.classList.add('hidden');
  }

  if (!hasData) return;

  // Progress
  const pos = p['time-pos'] ?? null;
  const dur = p['duration'] ?? null;
  const pct = p['percent-pos'] ?? 0;
  _currentPosition = pos;
  _currentDuration = dur;
  const progressPct = dur ? (pos / dur) * 100 : pct;
  if (!_isDraggingProgress) {
    if (_pendingSeekTarget && Date.now() < _pendingSeekTarget.expiry) {
      // Backend hasn't seeked yet; keep showing the target position.
      // Clear once the server position is within 3 s of where we dragged to.
      if (pos != null && Math.abs(pos - _pendingSeekTarget.seconds) < 3) {
        _pendingSeekTarget = null;
        updateProgressUi(progressPct, pos);
      } else {
        updateProgressUi(_pendingSeekTarget.percent, _pendingSeekTarget.seconds);
      }
    } else {
      _pendingSeekTarget = null;
      updateProgressUi(progressPct, pos);
    }
  }

  // Status & health
  const paused = p['pause'];
  const speed  = p['speed'];
  const subDly = p['sub-delay']   ?? 0;
  const audDly = p['audio-delay'] ?? 0;

  updateSpeedButtons(speed);

  // Health row
  const cacheState = p['demuxer-cache-state'] || {};
  const cacheDur = Number(cacheState['fw-duration'] ?? cacheState['cache-duration']);
  const underrun = Boolean(cacheState.underrun || cacheState['cache-underrun']);
  const cacheSpeedBps = Number(p['cache-speed'] ?? 0);

  // core-idle stays true while mpv is loading/buffering and has not yet
  // rendered the first frame. Before that first frame it's a cold start
  // (starting); a stall after playback had already begun is a distinct
  // rebuffering state, matching the Apple TV OSD pill.
  const coreIdle = Boolean(p['core-idle']);
  const pausedForCache = Boolean(p['paused-for-cache']);
  const isStarting = !paused && pos === 0 && (dur === null || dur === 0);
  const isStalled = underrun || pausedForCache
    || (!paused && coreIdle)
    || (!paused && Number.isFinite(cacheDur) && cacheDur < 0.3);
  const isBuffering = isStarting || isStalled;
  const badgeEl = document.getElementById('health-badge');
  if (badgeEl) {
    let badgeText = isStarting ? tr('starting') : (isStalled ? tr('buffering') : (paused ? tr('paused') : tr('playing')));
    if (!isBuffering && !paused && speed != null && Math.abs(Number(speed) - 1) > 0.01) {
      badgeText = `${tr('playing')} ${fmtSpeed(speed)}×`;
    }
    badgeEl.textContent = badgeText;
    badgeEl.className = 'info-tag' + (isBuffering ? ' warn' : (!paused ? ' accent' : ''));
  }

  // Inline cache + download speed next to time-current
  const dlSpeedText = cacheSpeedBps > 1000
    ? (cacheSpeedBps >= 1e6
        ? `↓${(cacheSpeedBps / 1e6).toFixed(1)}MB/s`
        : `↓${Math.round(cacheSpeedBps / 1e3)}KB/s`)
    : '';
  const cacheDurText = fmtCacheDur(cacheDur);
  const inlineParts = [cacheDurText, dlSpeedText].filter(Boolean);
  _setText('health-cache-speed', inlineParts.join(' '));

  // Cache buffer overlay on progress bar
  const cacheFill = document.getElementById('progress-cache-fill');
  if (cacheFill) {
    if (dur > 0 && Number.isFinite(cacheDur) && cacheDur > 0) {
      const cachePct = Math.min(100, (((pos ?? 0) + cacheDur) / dur) * 100);
      cacheFill.style.width = cachePct + '%';
    } else {
      cacheFill.style.width = '0%';
    }
  }

  // Status tags: aspect, speed, audio, subtitle
  const tracks = p['track-list'] || [];
  const audioTracks = tracks.filter(t => t.type === 'audio');
  const subTracks   = tracks.filter(t => t.type === 'sub');
  const audioTrack  = audioTracks.find(t => t.selected);
  const subTrack    = subTracks.find(t => t.selected);
  const _trackLbl   = t => t.title || (t.lang ? t.lang.toUpperCase() : `Track ${t.id}`);

  const aspectOpt = ASPECT_OPTIONS.find(o => o.value === _currentAspect);
  _setText('tag-aspect', _currentAspect !== 'fit' && aspectOpt ? aspectDisplay(aspectOpt) : '');
  _setText('tag-audio', (audioTrack && audioTracks.length > 1) ? _trackLbl(audioTrack) : '');
  const subNoneActive = subTracks.length > 0 && !subTracks.some(t => t.selected);
  _setText('tag-sub', subNoneActive ? tr('subtitlesOff') : (subTrack && subTracks.length > 1 ? _trackLbl(subTrack) : ''));

  document.getElementById('play-loading-tag')?.classList.add('hidden');

  // Pause/play button icon
  const pauseBtn = document.getElementById('btn-pause-play');
  if (pauseBtn) {
    pauseBtn.innerHTML = paused
      ? '<svg class="tpt-icon" viewBox="0 0 24 24" aria-hidden="true" style="padding-left:3px"><path d="M7.5 4.5l12 7.5-12 7.5V4.5z" fill="currentColor" stroke="none"/></svg>'
      : '<svg class="tpt-icon" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M8 5v14M16 5v14"/></svg>';
  }

  // Fine adj delay values
  _setText('info-sub-delay',   _fmtDelay(subDly));
  _setText('info-audio-delay', _fmtDelay(audDly));

  // Movie info line (duration shown when not a series)
  if (!currentItemIsSeries) {
    const movieInfo = document.getElementById('now-playing-movie-info');
    if (movieInfo && !movieInfo.classList.contains('hidden')) {
      const dur = p['duration'];
      const durStr = (dur != null && Number.isFinite(dur) && dur > 0) ? fmtRuntime(dur * 1e7) : '';
      movieInfo.textContent = durStr ? `${tr('movie')} · ${durStr}` : tr('movie');
    }
  }

  // Track lists
  renderTrackLists(p['track-list'] || []);

  if (_playbackInfoOpen) renderPlaybackInfoSheet(p);
}

function renderTrackLists(trackList) {
  _renderTrackPills(trackList.filter(t => t.type === 'sub'), 'sub-track-list', true);
  _renderTrackPills(trackList.filter(t => t.type === 'audio'), 'audio-track-list', false);
}

function _renderTrackPills(tracks, listId, hasNone) {
  const list = document.getElementById(listId);
  if (!list) return;
  if (!tracks.length) {
    list.innerHTML = `<span class="track-empty">${tr('noSwitchableTracks')}</span>`;
    return;
  }
  const pills = [];
  if (hasNone) {
    const noneActive = !tracks.some(t => t.selected);
    pills.push(`<button class="track-pill${noneActive ? ' active' : ''}" onclick="selectSubTrack('no')">${tr('trackNone')}</button>`);
  }
  for (const t of tracks) {
    const label = t.title || (t.lang ? t.lang.toUpperCase() : `Track ${t.id}`);
    const fn = hasNone ? `selectSubTrack(${t.id})` : `selectAudioTrack(${t.id})`;
    pills.push(`<button class="track-pill${t.selected ? ' active' : ''}" onclick="${fn}">${esc(label)}</button>`);
  }
  list.innerHTML = pills.join('');
}

function selectSubTrack(id) {
  cmd(['set_property', 'sid', id === 'no' ? 'no' : Number(id)]);
  closeSubTracksSheet();
}
function selectAudioTrack(id) {
  cmd(['set_property', 'aid', Number(id)]);
  closeAudioTracksSheet();
}

function updateProgressUi(percent, seconds) {
  const safePct = Number.isFinite(percent) ? Math.max(0, Math.min(100, percent)) : 0;
  const fill = document.getElementById('progress-fill');
  const thumb = document.getElementById('progress-thumb');
  const bar = document.getElementById('progress-bar');
  if (fill) fill.style.width = safePct + '%';
  if (thumb) thumb.style.left = safePct + '%';
  if (bar) {
    bar.setAttribute('aria-valuenow', String(Math.round(safePct)));
    bar.setAttribute('aria-valuetext', fmtTime(seconds));
  }
  document.getElementById('time-current').textContent = fmtTime(seconds);
  const remaining = (_currentDuration != null && seconds != null)
    ? _currentDuration - seconds : null;
  document.getElementById('time-total').textContent =
    remaining != null ? `-${fmtTime(Math.max(0, remaining))}` : '--:--';
}

function updateVolumeUi(volume) {
  const safeVol = Number.isFinite(Number(volume)) ? Math.max(0, Math.min(100, Number(volume))) : 0;
  const fill = document.getElementById('volume-fill');
  const thumb = document.getElementById('volume-thumb');
  const track = document.querySelector('.volume-track');
  if (fill) fill.style.width = safeVol + '%';
  if (thumb) thumb.style.left = safeVol + '%';
  if (track) {
    track.setAttribute('aria-valuenow', String(Math.round(safeVol)));
    track.setAttribute('aria-valuetext', `${Math.round(safeVol)}%`);
  }
}

function progressEventToTime(event) {
  if (!_currentDuration || !Number.isFinite(_currentDuration)) return null;
  const bar = document.getElementById('progress-bar');
  if (!bar) return null;
  const rect = bar.getBoundingClientRect();
  if (!rect.width) return null;
  const pct = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width));
  return {
    seconds: _currentDuration * pct,
    percent: pct * 100,
  };
}

function previewProgress(event) {
  const target = progressEventToTime(event);
  if (!target) return null;
  updateProgressUi(target.percent, target.seconds);
  return target;
}

function onProgressPointerDown(event) {
  const target = previewProgress(event);
  if (!target) return;
  _isDraggingProgress = true;
  _progressPointerId = event.pointerId;
  event.currentTarget.setPointerCapture?.(event.pointerId);
  event.preventDefault();
}

function onProgressPointerMove(event) {
  if (!_isDraggingProgress || event.pointerId !== _progressPointerId) return;
  previewProgress(event);
}

function onProgressPointerUp(event) {
  if (!_isDraggingProgress || event.pointerId !== _progressPointerId) return;
  const target = previewProgress(event);
  _isDraggingProgress = false;
  _progressPointerId = null;
  event.currentTarget.releasePointerCapture?.(event.pointerId);
  if (target) {
    // Keep the bar at the dragged position until the backend confirms the seek.
    _pendingSeekTarget = { seconds: target.seconds, percent: target.percent, expiry: Date.now() + 2500 };
    cmd(['seek', target.seconds, 'absolute']);
  }
}

function onProgressPointerCancel(event) {
  if (event.pointerId !== _progressPointerId) return;
  _isDraggingProgress = false;
  _progressPointerId = null;
}

function seekBackward() { cmd(['seek', -(_settings.seek_backward_secs || 5)]); }
function seekForward() { cmd(['seek', _settings.seek_forward_secs || 30]); }

function onProgressKeydown(event) {
  if (!_currentDuration) return;
  if (event.key === 'ArrowLeft') {
    event.preventDefault();
    cmd(['seek', -(_settings.seek_backward_secs || 5)]);
  } else if (event.key === 'ArrowRight') {
    event.preventDefault();
    cmd(['seek', _settings.seek_forward_secs || 30]);
  } else if (event.key === 'Home') {
    event.preventDefault();
    cmd(['seek', 0, 'absolute']);
  } else if (event.key === 'End') {
    event.preventDefault();
    cmd(['seek', _currentDuration, 'absolute']);
  }
}

function fmtSpeed(speed) {
  if (speed == null || !Number.isFinite(speed)) return '';
  return Number(speed).toFixed(2).replace(/\.?0+$/, '');
}

function fmtCacheDur(secs) {
  if (!Number.isFinite(secs) || secs <= 0) return '';
  if (secs < 60) return tr('cacheSec', { secs: Math.round(secs) });
  if (secs < 3600) return tr('cacheMin', { mins: (secs / 60).toFixed(1) });
  return tr('cacheHour', { hours: (secs / 3600).toFixed(1) });
}

function updateSpeedButtons(speed) {
  if (speed == null || !Number.isFinite(speed)) return;
  // Scope to the speed list only — the aspect sheet shares the .speed-row-btn
  // class but has no data-speed, so a global selector would clear its ✓ check
  // mark on every poll (the aspect checkmark flashing away after ~1s).
  document.querySelectorAll('#speed-list .speed-row-btn').forEach(btn => {
    const value = Number(btn.dataset.speed);
    btn.classList.toggle('active', Math.abs(value - speed) < 0.01);
  });
}

function openPlaybackInfoSheet() {
  _playbackInfoOpen = true;
  document.getElementById('playback-info-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
  renderPlaybackInfoSheet(_latestProps);
}

function closePlaybackInfoSheet() {
  _playbackInfoOpen = false;
  const backdrop = document.getElementById('playback-info-backdrop');
  if (!backdrop) return;
  backdrop.classList.add('hidden');
  document.body.classList.remove('sheet-open');
}

function onPlaybackInfoBackdropClick(event) {
  if (event.target.id === 'playback-info-backdrop') closePlaybackInfoSheet();
}

/* ── Sub tracks sheet ─────────────────────────────────────────────────────── */
function openSubTracksSheet() {
  _subTracksOpen = true;
  document.getElementById('sub-tracks-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
}
function closeSubTracksSheet() {
  _subTracksOpen = false;
  document.getElementById('sub-tracks-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
}
function onSubTracksBackdropClick(event) {
  if (event.target.id === 'sub-tracks-backdrop') closeSubTracksSheet();
}

/* ── Audio tracks sheet ───────────────────────────────────────────────────── */
function openAudioTracksSheet() {
  _audioTracksOpen = true;
  document.getElementById('audio-tracks-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
}
function closeAudioTracksSheet() {
  _audioTracksOpen = false;
  document.getElementById('audio-tracks-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
}
function onAudioTracksBackdropClick(event) {
  if (event.target.id === 'audio-tracks-backdrop') closeAudioTracksSheet();
}

/* ── Speed sheet ──────────────────────────────────────────────────────────── */
function openSpeedSheet() {
  _speedSheetOpen = true;
  document.getElementById('speed-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
}
function closeSpeedSheet() {
  _speedSheetOpen = false;
  document.getElementById('speed-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
}
function onSpeedBackdropClick(event) {
  if (event.target.id === 'speed-backdrop') closeSpeedSheet();
}

/* ── More sheet ───────────────────────────────────────────────────────────── */
function openMoreSheet() {
  _moreSheetOpen = true;
  document.getElementById('more-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
}
function closeMoreSheet() {
  _moreSheetOpen = false;
  document.getElementById('more-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
}
function onMoreBackdropClick(event) {
  if (event.target.id === 'more-backdrop') closeMoreSheet();
}

/* ── Aspect ratio sheet ───────────────────────────────────────────────────── */
function openAspectSheet() {
  _aspectSheetOpen = true;
  renderAspectSheet();
  document.getElementById('aspect-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
}

function closeAspectSheet() {
  _aspectSheetOpen = false;
  document.getElementById('aspect-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
}

function onAspectBackdropClick(event) {
  if (event.target.id === 'aspect-backdrop') closeAspectSheet();
}

function renderAspectSheet() {
  const list = document.getElementById('aspect-list');
  if (!list) return;
  list.innerHTML = ASPECT_OPTIONS.map(o => `
    <button class="speed-row-btn ${o.value === _currentAspect ? 'active' : ''}"
            onclick="setAspect('${o.value}')">
      <span>${esc(aspectLabel(o))}</span>
    </button>`).join('');
}


function setAspect(value) {
  if (value === 'fill') value = 'zoom';
  _currentAspect = value;
  // Always reset video-aspect-override first to clear any stale override,
  // then set only the properties relevant to the chosen mode.
  switch (value) {
    case 'original':
      cmd(['set_property', 'video-aspect-override', 'no']);
      cmd(['set_property', 'video-unscaled', 'yes']);
      cmd(['set_property', 'panscan', 0]);
      cmd(['set_property', 'keepaspect', true]);
      break;
    case 'zoom':
    case 'fill':
      cmd(['set_property', 'video-aspect-override', 'no']);
      cmd(['set_property', 'video-unscaled', 'no']);
      cmd(['set_property', 'keepaspect', true]);
      cmd(['set_property', 'panscan', 1]);
      break;
    case 'stretch':
      cmd(['set_property', 'video-aspect-override', 'no']);
      cmd(['set_property', 'video-unscaled', 'no']);
      cmd(['set_property', 'panscan', 0]);
      cmd(['set_property', 'keepaspect', false]);
      break;
    default: // 'fit'
      cmd(['set_property', 'video-aspect-override', 'no']);
      cmd(['set_property', 'video-unscaled', 'no']);
      cmd(['set_property', 'panscan', 0]);
      cmd(['set_property', 'keepaspect', true]);
      break;
  }
  const opt = ASPECT_OPTIONS.find(o => o.value === value);
  _setText('info-aspect', opt && opt.value !== 'fit' ? aspectDisplay(opt) : '');
  closeAspectSheet();
}

/* ── Loop mode ────────────────────────────────────────────────────────────── */
function toggleLoopMode() {
  _loopFile = !_loopFile;
  cmd(['set_property', 'loop-file', _loopFile ? 'yes' : 'no']);
  _updateLoopUI();
}

function _updateLoopUI() {
  const btn = document.getElementById('btn-loop');
  const label = document.getElementById('loop-label');
  if (label) label.textContent = _loopFile ? tr('singleLoop') : tr('sequentialPlay');
  if (btn) btn.classList.toggle('active', _loopFile);
}

function renderPlaybackInfoSheet(props = {}) {
  const content = document.getElementById('playback-info-content');
  if (!content) return;
  const d = buildPlaybackDiagnostics(props);
  content.innerHTML = `
    <section class="diagnostics-section diagnostics-first">
      <h3 id="playback-info-title">${tr('diagnosticsHealth')}</h3>
      <div class="health-metrics">
        ${healthMetricHtml(tr('buffer'), d.cacheDisplay, 's', '')}
        ${healthMetricHtml(tr('decoderDrops'), d.decoderDrops, '', '')}
        ${healthMetricHtml(tr('renderDrops'), d.renderDrops, '', '')}
        ${healthMetricHtml(tr('avSync'), d.avsyncDisplay, 'ms', d.avsyncTone)}
      </div>
      <div class="decode-method-row">
        <span>${tr('decodeMethod')}</span>
        <span class="decode-method-label">${esc(d.hwdecLabel)}</span>
        ${d.usingHwdec ? '<span class="decode-badge">HW</span>' : ''}
      </div>
    </section>
    <section class="diagnostics-section">
      <h3>${tr('sourceInfo')}</h3>
      <div class="diagnostics-kv2">
        ${kv2RowHtml(tr('resolution'), d.sourceResolution, tr('bitDepth'), d.bitDepth)}
        ${kv2RowHtml(tr('fps'), d.fps, tr('colorSpace'), d.colorSpace)}
        ${kv2RowHtml(tr('videoCodec'), d.videoCodec, tr('bitrate'), d.videoBitrateStr)}
        ${kv2RowHtml('HDR', d.hdrType, tr('container'), d.container)}
      </div>
    </section>
    <section class="diagnostics-section">
      <h3>${tr('decodeChain')}</h3>
      <div class="diagnostics-kv2">
        ${kv2RowHtml(tr('decoder'), d.decoderName, tr('outputFormat'), d.outputFormat)}
        ${kv2RowHtml(tr('hdrProcess'), d.hdrProcess, tr('scaleAlgo'), d.scaleAlgo)}
        ${kv2RowHtml(tr('renderResolution'), d.renderResolution, '', '')}
      </div>
    </section>
    <section class="diagnostics-section">
      <h3>${tr('audioInfo')}</h3>
      <div class="diagnostics-kv2">
        ${kv2RowHtml(tr('currentAudio'), d.audioTrackLabel, tr('sampleRate'), d.sampleRate)}
        ${kv2RowHtml(tr('audioCodec'), d.audioCodecLabel, tr('channelsLabel'), d.channelsLabel)}
        ${kv2RowHtml(tr('outputMode'), d.audioOutputMode, tr('outputDevice'), d.audioDevice)}
      </div>
    </section>`;
}

function buildPlaybackDiagnostics(p) {
  const videoParams = p['video-params'] || {};
  const targetParams = p['video-target-params'] || {};
  const trackList = Array.isArray(p['track-list']) ? p['track-list'] : [];
  const currentTracks = p['current-tracks'] || {};
  const currentHwdec = p['hwdec-current'] || '';
  const usingHwdec = Boolean(currentHwdec && currentHwdec !== 'no');

  const sourceHdrInfo = getHdrInfo(videoParams);
  const targetHdrInfo = getHdrInfo(targetParams);
  const hasHdrSource = sourceHdrInfo.isHdr;
  const hasHdrOutput = targetHdrInfo.isHdr;
  const trackText = JSON.stringify([trackList, currentTracks]).toLowerCase();
  const dolbyVision = /dolby.?vision|dovi|dvhe|dvh1|dvav|dav1/.test(trackText);

  // Cache
  const cache = p['demuxer-cache-state'] || {};
  const cacheDuration = Number(cache['fw-duration'] ?? cache['cache-duration']);
  const cacheDisplay = Number.isFinite(cacheDuration) && cacheDuration > 0
    ? cacheDuration.toFixed(1) : '—';

  // Drop frames
  const decoderDrops = Number.isFinite(Number(p['decoder-frame-drop-count']))
    ? String(Math.round(Number(p['decoder-frame-drop-count']))) : '—';
  const renderDrops = Number.isFinite(Number(p['vo-drop-frame-count']))
    ? String(Math.round(Number(p['vo-drop-frame-count']))) : '—';

  // AV sync
  let avsyncDisplay = '—', avsyncTone = '';
  const avsyncSec = p['avsync'];
  if (avsyncSec != null && Number.isFinite(Number(avsyncSec))) {
    const ms = Math.round(Number(avsyncSec) * 1000);
    avsyncDisplay = (ms > 0 ? '+' : '') + ms;
    avsyncTone = Math.abs(ms) <= 10 ? 'avsync-ok'
      : Math.abs(ms) <= 40 ? 'avsync-warn' : 'avsync-accent';
  }

  // Source resolution & bit depth
  const sourceResolution = (p.width && p.height) ? `${p.width}×${p.height}` : '—';
  const bitDepth = _fmtBitDepth(videoParams.pixelformat || '');
  const colorSpace = videoParams.primaries || '—';
  const hdrType = dolbyVision ? 'Dolby Vision'
    : hasHdrSource ? sourceHdrInfo.label : 'SDR';
  const fps = p['container-fps'] != null
    ? `${Number(p['container-fps']).toFixed(3)} fps` : '—';
  const videoCodec = _fmtVideoCodec(p['video-codec']);
  const videoBitrateStr = fmtBitrate(p['video-bitrate']);
  const container = _fmtContainer(p['file-format']);

  // Decode chain
  const hwdecLabel = usingHwdec ? _fmtHwdec(currentHwdec) : tr('softwareDecode');
  const decoderName = usingHwdec ? _fmtHwdecName(currentHwdec) : tr('softwareDecodeFfmpeg');
  const outputFormat = _fmtPixfmt(
    targetParams['hw-pixelformat'] || targetParams.pixelformat || ''
  );
  const hdrProcess = !hasHdrSource ? '—'
    : hasHdrOutput ? tr('passthrough') : tr('toneMap');
  const scaleAlgo = p['scale'] ? _fmtScaleAlgo(p['scale']) : '—';
  const renderResolution = (p['dwidth'] && p['dheight'])
    ? `${p['dwidth']}×${p['dheight']}` : '—';

  // Audio
  const currentAudio = currentTracks.audio
    || trackList.find(t => t.type === 'audio' && t.selected);
  let audioTrackLabel = '—';
  if (currentAudio) {
    const parts = [currentAudio.lang, currentAudio.title].filter(Boolean);
    audioTrackLabel = parts.length ? parts.join(' ') : '—';
  }
  const audioCodecLabel = _fmtAudioCodec(p['audio-codec']);
  const sampleRate = _fmtSampleRate(p['audio-samplerate']);
  const channelsLabel = _fmtChannelsShort(p['audio-channels']);
  const audioOutFmt = String((p['audio-out-params'] || {}).format || '').toLowerCase();
  const audioOutputMode = audioOutFmt.includes('spdif') ? tr('bitstream')
    : audioOutFmt ? tr('decodedOutput') : '—';
  const audioDevice = _fmtAudioDevice(p['audio-device'] || '');

  return {
    cacheDisplay, decoderDrops, renderDrops, avsyncDisplay, avsyncTone,
    usingHwdec, hwdecLabel,
    sourceResolution, bitDepth, fps, colorSpace,
    videoCodec, videoBitrateStr, hdrType, container,
    decoderName, outputFormat, hdrProcess, scaleAlgo, renderResolution,
    audioTrackLabel, audioCodecLabel, sampleRate, channelsLabel,
    audioOutputMode, audioDevice,
  };
}

function getHdrInfo(params = {}) {
  const gamma = String(params.gamma || '').toLowerCase();
  const primaries = String(params.primaries || '').toLowerCase();
  const sigPeak = Number(params['sig-peak'] || 0);
  const light = String(params.light || '').toLowerCase();
  const isPQ = gamma.includes('pq') || gamma.includes('smpte');
  const isHLG = gamma.includes('hlg');
  const isBt2020 = primaries.includes('2020') || primaries.includes('bt.2020');
  const isHdr = isPQ || isHLG || (isBt2020 && sigPeak > 1) || light.includes('hdr');
  const label = isPQ ? 'HDR10 / PQ'
    : isHLG ? 'HLG'
      : isHdr ? 'HDR'
        : 'SDR';
  return { isHdr, label };
}

function fmtColorLine(params = {}) {
  const parts = [
    params.primaries,
    params.gamma,
    params.colormatrix,
    params['sig-peak'] ? `peak ${Number(params['sig-peak']).toFixed(2)}` : '',
  ].filter(Boolean);
  return parts.length ? parts.join(' / ') : '—';
}

function fmtBitrate(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n <= 0) return '—';
  if (n >= 1000000) return `${(n / 1000000).toFixed(1)} Mbps`;
  if (n >= 1000) return `${Math.round(n / 1000)} kbps`;
  return `${Math.round(n)} bps`;
}

function healthMetricHtml(label, value, unit, tone) {
  const unitHtml = unit ? `<span class="health-metric-unit"> ${esc(unit)}</span>` : '';
  return `
    <div class="health-metric">
      <div class="health-metric-label">${esc(label)}</div>
      <div class="health-metric-value${tone ? ' ' + tone : ''}">${esc(value)}${unitHtml}</div>
    </div>`;
}

function kv2RowHtml(l1, v1, l2, v2) {
  const pair2 = l2
    ? `<div class="kv2-pair"><div class="kv2-label">${esc(l2)}</div><div class="kv2-value">${esc(v2 || '—')}</div></div>`
    : '<div class="kv2-pair kv2-empty"></div>';
  return `
    <div class="kv2-row">
      <div class="kv2-pair"><div class="kv2-label">${esc(l1)}</div><div class="kv2-value">${esc(v1 || '—')}</div></div>
      ${pair2}
    </div>`;
}

/* ── Formatting helpers ───────────────────────────────────────────────────── */
function fmtTime(secs) {
  if (secs == null || isNaN(secs)) return '--:--';
  secs = Math.floor(secs);
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  const s = secs % 60;
  return h > 0
    ? `${h}:${_pad(m)}:${_pad(s)}`
    : `${_pad(m)}:${_pad(s)}`;
}
function _pad(n) { return String(n).padStart(2, '0'); }
function _fmtDelay(s) { return (s > 0 ? '+' : '') + s.toFixed(1) + 's'; }
function fmtRuntime(ticks) {
  if (!ticks) return '';
  const mins = Math.round(ticks / 600000000);
  if (!mins) return '';
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  return h > 0
    ? tr('hoursMinutes', { hours: h, minutes: m ? tr('minutes', { minutes: m }) : '' }).trim()
    : tr('minutes', { minutes: m });
}

function episodeLabelForItem(item = {}) {
  const season = Number(item.ParentIndexNumber);
  const episode = Number(item.IndexNumber);
  if (Number.isFinite(season) && season > 0 && Number.isFinite(episode) && episode > 0) {
    return `S${_pad(season)}E${_pad(episode)}`;
  }
  if (Number.isFinite(episode) && episode > 0) return tr('episode', { num: episode });
  return '';
}

function _fmtVideoCodec(c) {
  if (!c) return '—';
  return { h264:'H.264 (AVC)', hevc:'H.265 (HEVC)', vp9:'VP9', av1:'AV1',
           mpeg2video:'MPEG-2', mpeg4:'MPEG-4', vc1:'VC-1' }[c] || c;
}

function _fmtAudioCodec(c) {
  if (!c) return '—';
  return { aac:'AAC', ac3:'AC-3 (Dolby Digital)', eac3:'E-AC-3 (Dolby Digital+)',
           dts:'DTS', truehd:'TrueHD (Atmos)', mlp:'MLP (TrueHD)',
           flac:`FLAC (${tr('lossless')})`, mp3:'MP3', vorbis:'Vorbis', opus:'Opus',
           pcm_s16le:'PCM 16bit', pcm_s24le:'PCM 24bit', pcm_s32le:'PCM 32bit' }[c]
         || c.toUpperCase();
}

function _fmtHwdec(h) {
  if (!h || h === 'no') return tr('softwareDecode');
  const copy = tr('copy');
  return { nvdec:'NVIDIA (nvdec)', 'nvdec-copy':`NVIDIA ${copy}`,
           cuda:'CUDA (NVIDIA)',
           d3d11va:'D3D11VA', 'd3d11va-copy':`D3D11VA ${copy}`,
           dxva2:'DXVA2',    'dxva2-copy':`DXVA2 ${copy}`,
           videotoolbox:'VideoToolbox (Apple)' }[h] || h;
}

function _fmtSampleRate(hz) {
  if (hz == null) return '—';
  return hz >= 1000 ? `${(hz / 1000).toFixed(1)} kHz` : `${hz} Hz`;
}

function _fmtChannels(n) {
  if (n == null) return '—';
  return { 1:tr('mono'), 2:tr('stereo'), 6:tr('surround51'), 8:tr('surround71') }[n]
       || tr('channels', { count: n });
}

function _fmtContainer(fmt) {
  if (!fmt) return '—';
  return { matroska:'MKV', 'matroska,webm':'MKV / WebM',
           'mov,mp4,m4a,3gp,3g2,mj2':'MP4', avi:'AVI',
           mpeg:'MPEG', mpegts:'MPEG-TS (TS)' }[fmt]
       || fmt.split(',')[0].toUpperCase();
}

function _fmtBitDepth(pixfmt) {
  if (!pixfmt) return '—';
  const m = String(pixfmt).match(/[p]?(\d+)(?:le|be)?$/);
  if (m) {
    const bits = parseInt(m[1]);
    if ([8, 10, 12, 16].includes(bits)) return `${bits}-bit`;
  }
  return '8-bit';
}

function _fmtHwdecName(h) {
  if (!h || h === 'no') return tr('softwareDecode');
  const copy = tr('copy');
  return {
    nvdec:'NVDEC', 'nvdec-copy':`NVDEC ${copy}`,
    cuda:'CUDA (NVIDIA)',
    d3d11va:'D3D11VA', 'd3d11va-copy':`D3D11VA ${copy}`,
    dxva2:'DXVA2', 'dxva2-copy':`DXVA2 ${copy}`,
    videotoolbox:'VideoToolbox',
  }[h] || h;
}

function _fmtPixfmt(fmt) {
  if (!fmt) return '—';
  return {
    p010:'P010', p010le:'P010', nv12:'NV12',
    yuv420p:'YUV420P', yuv420p10le:'YUV420P10', yuv420p12le:'YUV420P12',
    p016:'P016',
  }[fmt.toLowerCase()] || fmt.toUpperCase();
}

function _fmtScaleAlgo(s) {
  if (!s) return '—';
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function _fmtChannelsShort(n) {
  if (n == null) return '—';
  return { 1:tr('mono'), 2:'2.0', 6:'5.1', 7:'6.1', 8:'7.1' }[n] || `${n}ch`;
}

function _fmtAudioDevice(d) {
  if (!d) return '—';
  const clean = d.replace(/^(wasapi|pulse|coreaudio|alsa|auto)\//i, '');
  if (!clean || clean.toLowerCase() === 'auto') return tr('defaultDevice');
  return clean;
}

function _setText(id, val) {
  const el = document.getElementById(id);
  if (el) el.textContent = val || '';
}

/* ── Server switcher ──────────────────────────────────────────────────────── */
/* ── Source-type helpers ──────────────────────────────────────────────────── */
function _normType(type) {
  const t = String(type || 'emby').toLowerCase();
  return ['emby', 'jellyfin', 'plex', 'file'].includes(t) ? t : 'emby';
}

function _sourceBadge(type) {
  const t = _normType(type);
  const label = t === 'file' ? tr('typeFile') : (t.charAt(0).toUpperCase() + t.slice(1));
  return `<span class="source-badge source-${t}">${esc(label)}</span>`;
}

function _sourceMeta(s) {
  if (_normType(s.type) === 'file') {
    const proto = s.file_protocol || 'local';
    return s.root ? `${proto} · ${_truncate(s.root, 30)}` : proto;
  }
  const hosts = s.hosts || [];
  const host = hosts[s.active_host || 0] || '?';
  return `${host}:${s.port}`;
}

async function refreshServerSwitcher() {
  try {
    const servers = await api('GET', '/api/servers');
    const active  = servers.find(s => s.active);
    _activeSourceType = _normType(active && active.type);

    // Update header button
    const label = document.getElementById('active-server-label');
    const dot   = document.getElementById('server-dot');
    if (active) {
      label.textContent = active.name || tr('server');
      dot.classList.toggle('online', active.logged_in);
    } else {
      label.textContent = tr('notConfigured');
      dot.classList.remove('online');
    }

    // Render menu
    const menu = document.getElementById('server-menu');
    let html = servers.map(s => {
      const isFile = _normType(s.type) === 'file';
      const hosts = s.hosts || [];
      const activeHostIndex = s.active_host || 0;
      const hostButtons = (!isFile && hosts.length > 1)
        ? `<div class="server-menu-hosts">
            ${hosts.map((h, i) => `
              <button class="server-menu-host ${i === activeHostIndex ? 'active' : ''}"
                      onclick="switchServerHost(event, '${s.id}', ${i}, ${s.active})">
                ${esc(h)}
              </button>`).join('')}
          </div>`
        : '';
      return `<div class="server-menu-item ${s.active ? 'active' : ''}"
                   onclick="switchServer('${s.id}', '${jsStr(s.name || tr('server'))}')">
        <div class="smi-name">${esc(s.name || tr('server'))} ${_sourceBadge(s.type)}</div>
        <div class="smi-host">${esc(_sourceMeta(s))}</div>
        ${hostButtons}
      </div>`;
    }).join('');
    html += `<div class="server-menu-manage" onclick="openServerManager();closeServerMenu()">${tr('manageServers')}</div>`;
    menu.innerHTML = html || `<div class="server-menu-empty">${tr('noServers')}</div>`;

    // Render settings list
    renderServerList(servers);
  } catch (_) {}
}

function toggleServerMenu(e) {
  e.stopPropagation();
  document.getElementById('server-menu').classList.toggle('hidden');
}
function closeServerMenu() {
  document.getElementById('server-menu').classList.add('hidden');
}
function onDocClick() { closeServerMenu(); _closeEpisodePagePicker(); }

async function switchServer(serverId, serverName = '') {
  closeServerMenu();
  closeLibraryPicker();
  // Optimistic label update so header reflects the change immediately
  if (serverName) _setText('active-server-label', serverName);
  _showLibrarySkeletons();
  try {
    await api('POST', `/api/servers/${serverId}/activate`);
    await refreshServerSwitcher();
    await reloadLibraryForActiveServer();
  } catch (e) {
    toast(e.message, true);
    refreshServerSwitcher().catch(() => {});
  }
}

async function switchHost(serverId, hostIndex) {
  closeServerMenu();
  _showLibrarySkeletons();
  try {
    await api('PUT', `/api/servers/${serverId}/host`, { host_index: hostIndex });
    await refreshServerSwitcher();
    await reloadLibraryForActiveServer();
  } catch (e) { toast(e.message, true); }
}

async function switchServerHost(event, serverId, hostIndex, isActive = false) {
  event.stopPropagation();
  closeServerMenu();

  // Immediate visual feedback on the clicked element
  const clickedEl = event.currentTarget;
  if (clickedEl) { clickedEl.style.opacity = '0.45'; clickedEl.style.pointerEvents = 'none'; }
  toast(tr('switchingIp'));

  const stayOnRemote = isActive && _activeTab === 'remote';
  if (!stayOnRemote) {
    closeLibraryPicker();
    _showLibrarySkeletons();
  }

  try {
    await api('PUT', `/api/servers/${serverId}/host`, { host_index: hostIndex });
    if (!isActive) {
      await api('POST', `/api/servers/${serverId}/activate`);
    }
    await refreshServerSwitcher();
    if (!stayOnRemote) {
      await reloadLibraryForActiveServer();
    }
    toast(tr('ipSwitched'));
  } catch (e) {
    toast(e.message, true);
    if (clickedEl) { clickedEl.style.opacity = ''; clickedEl.style.pointerEvents = ''; }
  }
}

async function reloadLibraryForActiveServer(message = tr('loadingLibrary')) {
  resetSearchUi();
  if (_activeSourceType === 'file') {
    _enterFileMode();
    await loadDir('');
    return;
  }
  _exitFileMode();
  resetLibraryState();
  browseParentId = '';
  currentLibraryName = tr('all');
  _setViewMode('home');
  await loadLibraryNav();
  await loadHomeData();
}

/* ── Library selector ─────────────────────────────────────────────────────── */
async function loadLibraryNav() {
  const seq = ++libraryLoadSeq;
  libraryLoadError = '';
  setLibraryPickerBusy(true);
  try {
    const data = await api('GET', '/api/emby/libraries');
    if (seq !== libraryLoadSeq) return;
    const items = data.Items || [];
    libraryItems = [{ Id: '', Name: tr('all'), CollectionType: '' }, ...items.map(lib => ({
      Id: lib.Id || '',
      Name: lib.Name || tr('unnamedLibrary'),
      CollectionType: lib.CollectionType || '',
    }))];
    _homeLibraries = libraryItems.filter(l => l.Id !== '');
    if (!libraryItems.some(lib => lib.Id === browseParentId)) browseParentId = '';
    currentLibraryName = libraryItems.find(lib => lib.Id === browseParentId)?.Name || tr('all');
    updateLibraryPickerUi();
    renderLibraryPicker();
    renderCategoryPills();
  } catch (e) {
    if (seq !== libraryLoadSeq) return;
    libraryItems = [{ Id: '', Name: tr('all'), CollectionType: '' }];
    _homeLibraries = [];
    libraryLoadError = e.message || tr('loadingLibraryFailed');
    browseParentId = '';
    currentLibraryName = tr('all');
    updateLibraryPickerUi();
    renderLibraryPicker();
    toast(libraryLoadError, true);
  } finally {
    if (seq === libraryLoadSeq) setLibraryPickerBusy(false);
  }
}

function _showLibrarySkeletons() {
  resetLibraryState();
  _setViewMode('home');
  switchTab('library');
  const container = document.getElementById('home-type-sections');
  if (container) {
    const skeletonSection = () => `
      <section class="home-section">
        <div class="section-header">
          <div class="skeleton-line skeleton-shine" style="height:13px;width:68px;border-radius:6px"></div>
        </div>
        <div class="poster-row-4">${Array(4).fill(skeletonPosterCardHtml()).join('')}</div>
      </section>`;
    container.innerHTML = skeletonSection() + skeletonSection();
  }
  document.getElementById('section-resume')?.classList.add('hidden');
}

function resetLibraryState() {
  libraryItems = [{ Id: '', Name: tr('all'), CollectionType: '' }];
  _homeLibraries = [];
  currentLibraryName = tr('all');
  libraryLoadError = '';
  browseParentId = '';
  browseStart = 0;
  browseHasMore = false;
  updateLibraryPickerUi();
  renderLibraryPicker();
  renderCategoryPills();
  document.getElementById('home-type-sections').innerHTML = '';
}

function updateLibraryPickerUi() {
  renderLibraryTabs();
}

function renderLibraryTabs() {
  const tabs = document.getElementById('library-tabs');
  if (!tabs) return;
  tabs.innerHTML = libraryItems.map(lib => `
    <button class="library-tab${lib.Id === browseParentId ? ' active' : ''}"
            onclick="switchLibrary('${lib.Id}', '${jsStr(lib.Name)}')">${esc(lib.Name)}</button>
  `).join('');
  requestAnimationFrame(() => {
    const active = tabs.querySelector('.library-tab.active');
    if (active) active.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' });
  });
}

/* ── Category pills (home view) ───────────────────────────────────────────── */
const _COLLECTION_ICONS = {
  movies: `<svg class="category-chip-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="4.5" width="20" height="15" rx="2.5"/><path d="M7 4.5v15M17 4.5v15M2 10h5M2 15h5M17 10h5M17 15h5"/></svg>`,
  tvshows: `<svg class="category-chip-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3.5" width="20" height="14" rx="2.5"/><path d="M8 21.5h8M12 17.5v4"/></svg>`,
  boxsets: `<svg class="category-chip-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="8" height="8" rx="1.5"/><rect x="13" y="3" width="8" height="8" rx="1.5"/><rect x="3" y="13" width="8" height="8" rx="1.5"/><rect x="13" y="13" width="8" height="8" rx="1.5"/></svg>`,
  playlists: `<svg class="category-chip-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M3 12h12"/><circle cx="17.5" cy="16.5" r="3.5"/><path d="M21 13v3.5"/></svg>`,
  music: `<svg class="category-chip-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>`,
  homevideos: `<svg class="category-chip-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m3 9 9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/></svg>`,
  _default: `<svg class="category-chip-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>`,
};

function renderCategoryPills() {
  const row = document.getElementById('category-row');
  if (!row) return;
  if (!_homeLibraries.length) {
    row.innerHTML = `<div class="category-row-loading">${tr('noLibrary')}</div>`;
    return;
  }
  row.innerHTML = _homeLibraries.map(lib => {
    const icon = _COLLECTION_ICONS[lib.CollectionType] || _COLLECTION_ICONS._default;
    const isActive = lib.Id === browseParentId;
    return `<button class="category-chip${isActive ? ' active' : ''}"
             id="cat-chip-${lib.Id}"
             onclick="switchLibrary('${lib.Id}', '${jsStr(lib.Name)}')">
      ${icon}
      <span class="category-chip-name">${esc(lib.Name)}</span>
      <span class="category-chip-count" id="cat-count-${lib.Id}"></span>
    </button>`;
  }).join('');
}

function _updateCategoryChipCount(libId, count) {
  const el = document.getElementById(`cat-count-${libId}`);
  if (el) el.textContent = count > 0 ? String(count) : '';
}

function _updateCategoryChipActive() {
  document.querySelectorAll('.category-chip').forEach(chip => {
    const id = chip.id.replace('cat-chip-', '');
    chip.classList.toggle('active', id === browseParentId && browseParentId !== '');
  });
}

/* ── Home data ────────────────────────────────────────────────────────────── */
async function loadHomeData() {
  await Promise.all([loadResume(), loadHomeTypeSections(), loadAllCategoryChipCounts()]);
}

async function loadAllCategoryChipCounts() {
  await Promise.all(_homeLibraries.map(async lib => {
    if (!lib.Id) return;
    try {
      const qs = new URLSearchParams({ start: 0, limit: 1, parent_id: lib.Id });
      const data = await api('GET', `/api/emby/items?${qs}`);
      _updateCategoryChipCount(lib.Id, data.TotalRecordCount || 0);
    } catch (_) {}
  }));
}

/* ── Collection type sections on home ────────────────────────────────────── */
const _COLLECTION_TYPE_LABEL = {
  movies: 'movie', tvshows: 'series', boxsets: 'boxset',
  playlists: 'playlist', music: 'music', homevideos: 'homeVideos',
};

async function loadHomeTypeSections() {
  const container = document.getElementById('home-type-sections');
  if (!container) return;

  // Group libraries by CollectionType; keep order from Emby
  const seen = new Set();
  const groups = [];
  for (const lib of _homeLibraries) {
    const type = lib.CollectionType || '_other';
    if (!seen.has(type)) { seen.add(type); groups.push({ type, libs: [] }); }
    groups.find(g => g.type === type).libs.push(lib);
  }

  if (!groups.length) { container.innerHTML = ''; return; }

  // Render skeletons
  container.innerHTML = groups.map(g => {
    const label = _COLLECTION_TYPE_LABEL[g.type] ? tr(_COLLECTION_TYPE_LABEL[g.type]) : (g.libs[0]?.Name || g.type);
    const skeletonRow = Array(4).fill(skeletonPosterCardHtml()).join('');
    return `
      <section class="home-section" id="home-sec-${g.type}">
        <div class="section-header">
          <h2 class="section-title-h">${esc(label)}</h2>
          <button class="section-more-btn" onclick="viewAllByType('${g.type}')">
            ${tr('viewAll')} <svg class="chevron-sm" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="m9 18 6-6-6-6"/></svg>
          </button>
        </div>
        <div class="poster-row-4" id="home-row-${g.type}">
          ${skeletonRow}
        </div>
      </section>`;
  }).join('');

  // Load items for each group in parallel
  await Promise.all(groups.map(async g => {
    const row = document.getElementById(`home-row-${g.type}`);
    if (!row) return;
    try {
      const firstLib = g.libs[0];
      const qs = new URLSearchParams({ start: 0, limit: 8 });
      if (firstLib?.Id) qs.set('parent_id', firstLib.Id);
      const data = await api('GET', `/api/emby/items?${qs}`);
      const items = (data.Items || []).slice(0, 8);
      if (!items.length) {
        document.getElementById(`home-sec-${g.type}`)?.classList.add('hidden');
        return;
      }
      row.innerHTML = items.map(item => posterCardHtml(item)).join('');
    } catch (_) {
      if (row) row.innerHTML = `<div class="row-error">${tr('loadFailed')}</div>`;
    }
  }));
}

function viewAllByType(type) {
  // Match the same grouping key loadHomeTypeSections uses; libraries with no
  // CollectionType are grouped under '_other' and previously matched nothing,
  // so their View All button did nothing.
  const lib = _homeLibraries.find(l => (l.CollectionType || '_other') === type);
  if (lib) switchLibrary(lib.Id, lib.Name);
}

function viewAllResume() {
  // Currently no dedicated "all resume" view — no-op
}

function setLibraryPickerBusy(isBusy) {
  const toolbar = document.getElementById('library-toolbar');
  if (toolbar) toolbar.classList.toggle('loading', isBusy);
}

function openLibraryPicker() {
  const backdrop = document.getElementById('library-picker-backdrop');
  backdrop.classList.remove('hidden');
  document.body.classList.add('sheet-open');
  renderLibraryPicker();
}

function closeLibraryPicker() {
  const backdrop = document.getElementById('library-picker-backdrop');
  if (!backdrop) return;
  backdrop.classList.add('hidden');
  document.body.classList.remove('sheet-open');
}

function onLibraryPickerBackdropClick(event) {
  if (event.target.id === 'library-picker-backdrop') closeLibraryPicker();
}

function renderLibraryPicker() {
  const list = document.getElementById('library-picker-list');
  if (!list) return;
  if (libraryLoadError) {
    list.innerHTML = `<div class="sheet-state compact">${esc(libraryLoadError)}</div>`;
    return;
  }
  list.innerHTML = libraryItems.map(lib => `
    <button class="library-option ${lib.Id === browseParentId ? 'active' : ''}"
            onclick="switchLibrary('${lib.Id}', '${jsStr(lib.Name)}')">
      <span>${esc(lib.Name)}</span>
      ${lib.Id === browseParentId ? `<span class="library-option-mark">${tr('current')}</span>` : ''}
    </button>
  `).join('');
}

async function switchLibrary(libId, name) {
  resetSearchUi();
  closeLibraryPicker();
  browseParentId = libId;
  currentLibraryName = name || tr('all');
  updateLibraryPickerUi();
  _updateCategoryChipActive();

  if (!libId) {
    // Back to home
    _setViewMode('home');
  } else {
    _setViewMode('browse');
    browseStart = 0;
    setBrowseTitle();
    await loadBrowse(false, { loadingMessage: tr('loadingCategory', { name: currentLibraryName }) });
  }
}

async function browseAll() {
  await switchLibrary('', tr('all'));
}

/* ── Browse grid ──────────────────────────────────────────────────────────── */
async function loadBrowse(append = false, opts = {}) {
  const requestId = ++browseLoadSeq;
  if (!append) browseStart = 0;
  const btn = document.getElementById('btn-load-more');
  if (!append) {
    renderBrowseLoading(opts.loadingMessage || tr('loadingCategory', { name: currentLibraryName || tr('library') }));
  } else if (btn) {
    btn.disabled = true;
    btn.textContent = tr('loadingMore');
  }
  try {
    const qs = new URLSearchParams({ start: browseStart, limit: PAGE_SIZE });
    if (browseParentId) qs.set('parent_id', browseParentId);
    const data  = await api('GET', `/api/emby/items?${qs}`);
    if (requestId !== browseLoadSeq) return;
    const items = data.Items || [];
    browseHasMore = browseStart + items.length < (data.TotalRecordCount || 0);
    browseStart  += items.length;

    const grid = document.getElementById('poster-grid');
    if (!append) grid.innerHTML = '';
    if (!items.length && !append) {
      grid.innerHTML = `<div class="search-state"><div class="search-state-title">${tr('noBrowseContentTitle')}</div><div class="search-state-subtitle">${tr('noBrowseContentSub')}</div></div>`;
    } else {
      grid.insertAdjacentHTML('beforeend', items.map(posterCardHtml).join(''));
    }

    if (btn) btn.classList.toggle('hidden', !browseHasMore);
  } catch (e) {
    if (requestId !== browseLoadSeq) return;
    renderBrowseError(e.message);
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.textContent = tr('loadMore');
    }
  }
}

function loadMore() { loadBrowse(true); }

function setBrowseTitle() {
  document.getElementById('browse-title').textContent =
    browseParentId ? currentLibraryName : tr('browse');
}

function renderBrowseLoading(_message) {
  setBrowseTitle();
  const grid = document.getElementById('poster-grid');
  if (grid) grid.innerHTML = Array(12).fill(skeletonPosterCardHtml()).join('');
  document.getElementById('btn-load-more')?.classList.add('hidden');
}

function renderBrowseError(message) {
  const grid = document.getElementById('poster-grid');
  if (grid) grid.innerHTML = `
    <div class="search-state">
      <div class="search-state-title">${tr('libraryLoadFailed')}</div>
      <div class="search-state-subtitle">${esc(message || tr('retryLater'))}</div>
    </div>`;
  document.getElementById('btn-load-more')?.classList.add('hidden');
}

function markPosterMissing(img) {
  const frame = img.closest('.poster-frame, .resume-poster-frame, .sheet-poster-frame, .remote-poster-card');
  if (frame) frame.classList.add('missing');
  img.remove();
}

function posterCardHtml(item) {
  const isSeries = item.Type === 'Series';
  const title    = item.Name || '';
  const year     = item.ProductionYear ? `<div class="poster-year">${item.ProductionYear}</div>` : '';
  const rating   = item.CommunityRating
    ? `<div class="poster-rating"><svg class="star-icon" viewBox="0 0 24 24"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/></svg>${Number(item.CommunityRating).toFixed(1)}</div>`
    : '';
  const epCount  = isSeries && item.RecursiveItemCount
    ? `<div class="poster-ep-count">${tr('totalEpisodes', { count: item.RecursiveItemCount })}</div>`
    : '';
  return `
    <div class="poster-card" onclick="openVideoSheet('${item.Id}')">
      ${posterFrameHtml(item.Id, title, 'poster-frame', 300, epCount, rating)}
      <div class="poster-card-info">
        <div class="poster-title">${esc(title)}</div>
        ${year}
      </div>
    </div>`;
}

function posterFrameHtml(itemId, title, className = 'poster-frame', maxHeight = 300, topOverlay = '', bottomOverlay = '') {
  return `
    <div class="${className}">
      <div class="poster-placeholder" aria-hidden="true">
        <span class="poster-placeholder-mark"></span>
        <span>${tr('noPoster')}</span>
      </div>
      <img src="/api/emby/image/${encodeURIComponent(itemId)}?max_height=${maxHeight}"
           alt="${esc(title)}" loading="lazy" onerror="markPosterMissing(this)">
      ${topOverlay}${bottomOverlay}
    </div>`;
}

/* ── Recently watched ─────────────────────────────────────────────────────── */
async function loadResume() {
  const row = document.getElementById('resume-row');
  const sec = document.getElementById('section-resume');
  if (!row) return;
  sec?.classList.remove('hidden');
  row.innerHTML = Array(3).fill(skeletonResumeCardHtml()).join('');
  try {
    const data  = await api('GET', '/api/emby/resume');
    const items = data.Items || [];
    if (!items.length) { sec?.classList.add('hidden'); row.innerHTML = ''; return; }
    row.innerHTML = items.map(resumeCardHtml).join('');
  } catch (_) { sec?.classList.add('hidden'); row.innerHTML = ''; }
}

function resumeCardHtml(item) {
  const userData    = item.UserData || {};
  const pct         = Math.round(userData.PlayedPercentage || 0);
  const seriesTitle = item.SeriesName || '';
  const title       = item.Name || '';
  const episodeLabel = episodeLabelForItem(item);
  const mainLabel   = seriesTitle || title;
  const subLabel    = seriesTitle ? (episodeLabel || title) : (item.ProductionYear || '');
  const posTicksRaw = userData.PlaybackPositionTicks;
  const durTicksRaw = item.RunTimeTicks;
  const posSecs  = posTicksRaw ? posTicksRaw / 1e7 : null;
  const durSecs  = durTicksRaw ? durTicksRaw / 1e7 : null;
  // Fallback: derive position from percentage when PlaybackPositionTicks is missing
  const derivedPosSecs = posSecs || (pct > 0 && durSecs ? (pct / 100) * durSecs : null);
  const timeStr  = (derivedPosSecs && derivedPosSecs > 0 && durSecs) ? `${fmtTime(derivedPosSecs)} / ${fmtTime(durSecs)}` : '';
  const showProgress = pct > 0 || (derivedPosSecs && derivedPosSecs > 0 && durSecs);
  // Use series poster for episodes; try Backdrop image (falls back to Primary server-side)
  const posterItemId = item.SeriesId || item.Id;
  const imgSrc = `/api/emby/image/${encodeURIComponent(posterItemId)}?max_height=400&type=Backdrop`;
  return `
    <div class="resume-card" onclick="openVideoSheet('${item.Id}')">
      <div class="resume-poster-frame" id="rf-${item.Id}">
        <img src="${imgSrc}" alt="${esc(title)}" loading="lazy" onerror="markPosterMissing(this)">
        <div class="resume-gradient"></div>
        <button class="resume-play-btn" onclick="event.stopPropagation();openVideoSheet('${item.Id}')" aria-label="${tr('play')}">
          <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5.14v13.72L19 12 8 5.14z"/></svg>
        </button>
        <div class="resume-overlay">
          <div class="resume-info">
            <div class="resume-title">${esc(mainLabel)}</div>
            ${subLabel ? `<div class="resume-subtitle">${esc(subLabel)}</div>` : ''}
          </div>
        </div>
        ${showProgress ? `
          <div class="resume-progress-wrap">
            ${timeStr ? `<div class="resume-time-row">${esc(timeStr)}</div>` : ''}
            ${pct > 0 ? `<div class="resume-progress-bar-outer">
              <div class="resume-progress-bar-fill" style="width:${pct}%"></div>
            </div>` : ''}
          </div>` : ''}
      </div>
    </div>`;
}

/* ── Video sheet ──────────────────────────────────────────────────────────── */
function onSheetBackdropClick(event) {
  if (event.target.id === 'video-sheet-backdrop') closeVideoSheet();
}

function openSheetShell() {
  const backdrop = document.getElementById('video-sheet-backdrop');
  backdrop.classList.remove('hidden');
  document.body.classList.add('sheet-open');
}

function closeVideoSheet() {
  const backdrop = document.getElementById('video-sheet-backdrop');
  if (!backdrop) return;
  backdrop.classList.add('hidden');
  document.body.classList.remove('sheet-open');
  sheetSeriesId = '';
  sheetEpisodesTotal = 0;
  sheetEpisodesLoading = false;
  _sheetEpisodePage = 0;
}

async function openVideoSheet(itemId) {
  openSheetShell();
  const content = document.getElementById('video-sheet-content');
  content.innerHTML = sheetLoadingHtml();
  _locateEpisode = null;
  try {
    const item = await api('GET', `/api/emby/items/${encodeURIComponent(itemId)}`);
    if (item.Type === 'Episode' && item.SeriesId) {
      // Open the parent series sheet and scroll to this episode.
      const series = await api('GET', `/api/emby/items/${encodeURIComponent(item.SeriesId)}`);
      _locateEpisode = { id: item.Id, num: Number(item.IndexNumber) || null };
      renderSeriesSheet(series);
      await loadSheetEpisodes(series.Id);
      _locateEpisode = null;
    } else if (item.Type === 'Series') {
      renderSeriesSheet(item);
      await loadSheetEpisodes(item.Id);
    } else if (item.Type === 'BoxSet') {
      renderBoxSetSheet(item);
      await loadBoxSetItems(item.Id);
    } else {
      renderPlayableSheet(item);
    }
  } catch (e) {
    content.innerHTML = `<div class="sheet-state">${tr('loadFailed')}: ${esc(e.message)}</div>`;
  }
}

function renderPlayableSheet(item) {
  const title = item.Name || '';
  const seriesTitle = item.SeriesName || '';
  const episodeLabel = episodeLabelForItem(item);
  const subtitle = seriesTitle ? `${seriesTitle}${episodeLabel ? ' · ' + episodeLabel : ''}` : '';
  const seriesId = item.SeriesId || '';
  const seasonId = item.SeasonId || '';
  const posterItemId = seriesId || item.Id;
  document.getElementById('video-sheet-content').innerHTML = `
    ${sheetHeroHtml(item, subtitle)}
    <div class="sheet-actions">
      <button class="sheet-play" onclick="playItem('${item.Id}', '${seriesId}', '${seasonId}', '${jsStr(title)}', '${jsStr(seriesTitle)}', '${jsStr(episodeLabel)}', '${jsStr(posterItemId)}')">${tr('play')}</button>
    </div>`;
}

function _pageForEpisodeNum(num) {
  return (num && num > 1) ? Math.floor((num - 1) / EPISODE_PAGE_SIZE) : 0;
}

function _estimateEpisodePage() {
  if (!currentEpisodeLabel) return 0;
  let epNum = null;
  const m1 = /E(\d+)/i.exec(currentEpisodeLabel);
  if (m1) epNum = Number(m1[1]);
  else {
    const m2 = /(?:\u7b2c\s*(\d+)\s*\u96c6|(?:EP|Episode)\s*(\d+))/i.exec(currentEpisodeLabel);
    if (m2) epNum = Number(m2[1] || m2[2]);
  }
  return _pageForEpisodeNum(epNum);
}

function renderSeriesSheet(item) {
  sheetSeriesId = item.Id;
  sheetEpisodesTotal = 0;
  sheetEpisodesLoading = false;
  // Jump to the page containing the episode we want to locate — either an
  // explicit target (for example a recent card) or the current episode.
  _sheetEpisodePage = (_locateEpisode && _locateEpisode.num)
    ? _pageForEpisodeNum(_locateEpisode.num)
    : ((currentSeriesId === item.Id && currentItemId) ? _estimateEpisodePage() : 0);
  currentSeriesId = item.Id;
  currentSeriesTitle = item.Name || '';
  const sortLabel = _sheetEpisodeSort === 'asc' ? tr('sortAsc') : tr('sortDesc');
  document.getElementById('video-sheet-content').innerHTML = `
    ${sheetHeroHtml(item, tr('series'))}
    <div class="episode-tools">
      <button class="episode-sort-btn" id="episode-sort-btn" onclick="toggleEpisodeSort()">${sortLabel}</button>
      <div class="episode-count-wrap">
        <button class="episode-page-picker-btn hidden" id="episode-page-picker-btn"
                onclick="toggleEpisodePagePicker(event)" aria-expanded="false">
          <span id="episode-total-label">${tr('loadingMore')}</span>
          <svg class="ep-picker-chevron" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="m6 9 6 6 6-6"/></svg>
        </button>
        <div class="episode-page-picker hidden" id="episode-page-picker"></div>
      </div>
    </div>
    <div class="sheet-episodes" id="sheet-episodes"></div>`;
}

function sheetHeroHtml(item, subtitle = '') {
  const title = item.Name || '';
  const meta = [
    item.ProductionYear || '',
    fmtRuntime(item.RunTimeTicks),
    (item.Genres || []).slice(0, 2).join(' / '),
  ].filter(Boolean).join(' · ');
  const overview = item.Overview || tr('noOverview');
  return `
    <div class="sheet-hero">
      ${posterFrameHtml(item.Id, title, 'sheet-poster-frame', 420)}
      <div class="sheet-main">
        ${subtitle ? `<div class="sheet-subtitle">${esc(subtitle)}</div>` : ''}
        <h2 class="sheet-title" id="sheet-title">${esc(title)}</h2>
        ${meta ? `<div class="sheet-meta">${esc(meta)}</div>` : ''}
      </div>
    </div>
    <p class="sheet-overview">${esc(overview)}</p>`;
}

function sheetLoadingHtml() {
  return `
    <div class="sheet-hero">
      <div class="sheet-poster-frame skeleton-poster-card skeleton-shine" style="border-radius:var(--radius)"></div>
      <div class="sheet-main" style="display:flex;flex-direction:column;gap:8px;justify-content:flex-end">
        <div class="skeleton-line skeleton-shine" style="height:9px;width:42%;border-radius:4px"></div>
        <div class="skeleton-line skeleton-shine" style="height:20px;width:88%;border-radius:6px"></div>
        <div class="skeleton-line skeleton-shine" style="height:9px;width:60%;border-radius:4px"></div>
      </div>
    </div>
    <div style="display:flex;flex-direction:column;gap:7px;margin-top:16px">
      <div class="skeleton-line skeleton-shine" style="height:9px;width:100%;border-radius:4px"></div>
      <div class="skeleton-line skeleton-shine" style="height:9px;width:86%;border-radius:4px"></div>
      <div class="skeleton-line skeleton-shine" style="height:9px;width:70%;border-radius:4px"></div>
    </div>`;
}

function skeletonEpisodeListHtml() {
  const row = `
    <div style="display:flex;align-items:center;gap:12px;padding:14px 16px;border-bottom:1px solid rgba(255,255,255,.05)">
      <div class="skeleton-line skeleton-shine" style="width:32px;height:12px;border-radius:4px;flex-shrink:0"></div>
      <div style="flex:1;display:flex;flex-direction:column;gap:6px">
        <div class="skeleton-line skeleton-shine" style="height:11px;width:72%;border-radius:4px"></div>
        <div class="skeleton-line skeleton-shine" style="height:9px;width:42%;border-radius:4px"></div>
      </div>
    </div>`;
  return Array(6).fill(row).join('');
}

function renderBoxSetSheet(item) {
  document.getElementById('video-sheet-content').innerHTML = `
    ${sheetHeroHtml(item, tr('boxset'))}
    <div class="episode-tools" style="margin-top:16px">
      <div class="episode-tools-title" id="boxset-count-title">${tr('loadingMore')}</div>
    </div>
    <div class="poster-row-4" id="boxset-items-grid" style="margin-top:8px;padding:0 0 4px"></div>`;
}

async function loadBoxSetItems(boxSetId) {
  const grid = document.getElementById('boxset-items-grid');
  const title = document.getElementById('boxset-count-title');
  if (!grid) return;
  grid.innerHTML = Array(4).fill(skeletonPosterCardHtml()).join('');
  try {
    const qs = new URLSearchParams({ parent_id: boxSetId, start: 0, limit: 60 });
    const data = await api('GET', `/api/emby/items?${qs}`);
    const items = (data.Items || []);
    if (title) title.textContent = tr('includeCount', { count: items.length });
    if (!items.length) {
      grid.innerHTML = `<div class="row-error" style="grid-column:1/-1;padding:12px;text-align:center;color:rgba(255,255,255,.4);font-size:13px">${tr('noContent')}</div>`;
      return;
    }
    grid.innerHTML = items.map(posterCardHtml).join('');
  } catch (e) {
    if (grid) grid.innerHTML = `<div class="row-error" style="grid-column:1/-1;padding:12px;text-align:center;color:rgba(255,255,255,.4);font-size:13px">${tr('loadFailed')}: ${esc(e.message)}</div>`;
  }
}

async function loadSheetEpisodes(seriesId) {
  if (!seriesId || sheetEpisodesLoading) return;
  const list = document.getElementById('sheet-episodes');
  if (!list) return;

  list.innerHTML = skeletonEpisodeListHtml();
  sheetEpisodesLoading = true;
  updateSheetEpisodeControls();

  const pageStart = _sheetEpisodePage * EPISODE_PAGE_SIZE;
  try {
    const qs = new URLSearchParams({
      series_id: seriesId,
      start: pageStart,
      limit: EPISODE_PAGE_SIZE,
      sort: _sheetEpisodeSort,
    });
    const data = await api('GET', `/api/emby/episodes?${qs}`);
    const items = data.Items || [];
    sheetEpisodesTotal = data.TotalRecordCount || items.length;
    list.innerHTML = '';
    if (!items.length) {
      list.innerHTML = `<div class="sheet-state compact">${tr('noEpisodes')}</div>`;
    } else {
      list.innerHTML = items.map((ep, i) => sheetEpisodeHtml(ep, pageStart + i)).join('');
      let targetEl = null;
      if (_locateEpisode && _locateEpisode.id) {
        targetEl = list.querySelector(`.sheet-episode[data-item-id="${_locateEpisode.id}"]`);
        if (targetEl) targetEl.classList.add('locate');
      }
      targetEl = targetEl || list.querySelector('.sheet-episode.playing');
      if (targetEl) targetEl.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }
  } catch (e) {
    list.innerHTML = `<div class="sheet-state compact">${tr('episodesLoadFailed', { message: esc(e.message) })}</div>`;
  } finally {
    sheetEpisodesLoading = false;
    updateSheetEpisodeControls();
  }
}

function updateSheetEpisodeControls() {
  const totalLabel = document.getElementById('episode-total-label');
  if (totalLabel) {
    totalLabel.textContent = sheetEpisodesTotal > 0 ? tr('totalEpisodes', { count: sheetEpisodesTotal }) : tr('episodes');
  }
  const sortBtn = document.getElementById('episode-sort-btn');
  if (sortBtn) sortBtn.textContent = _sheetEpisodeSort === 'asc' ? tr('sortAsc') : tr('sortDesc');

  const totalPages = sheetEpisodesTotal ? Math.ceil(sheetEpisodesTotal / EPISODE_PAGE_SIZE) : 0;
  const pickerBtn = document.getElementById('episode-page-picker-btn');
  if (pickerBtn) pickerBtn.classList.toggle('hidden', totalPages <= 1);
  renderEpisodePagePicker();
}

function renderEpisodePagePicker() {
  const picker = document.getElementById('episode-page-picker');
  if (!picker) return;
  const totalPages = sheetEpisodesTotal ? Math.ceil(sheetEpisodesTotal / EPISODE_PAGE_SIZE) : 0;
  if (totalPages <= 1) { picker.innerHTML = ''; return; }
  const btns = [];
  for (let i = 0; i < totalPages; i++) {
    const s = i * EPISODE_PAGE_SIZE + 1;
    const e = Math.min((i + 1) * EPISODE_PAGE_SIZE, sheetEpisodesTotal);
    btns.push(`<button class="ep-page-range-btn${i === _sheetEpisodePage ? ' active' : ''}"
               onclick="jumpToEpisodePage(${i})">${s}–${e}</button>`);
  }
  picker.innerHTML = btns.join('');
}

function toggleEpisodePagePicker(event) {
  event.stopPropagation();
  const picker = document.getElementById('episode-page-picker');
  const btn = document.getElementById('episode-page-picker-btn');
  if (!picker || !btn) return;
  const isOpen = !picker.classList.contains('hidden');
  picker.classList.toggle('hidden', isOpen);
  btn.setAttribute('aria-expanded', String(!isOpen));
}

function jumpToEpisodePage(pageIndex) {
  _sheetEpisodePage = pageIndex;
  _closeEpisodePagePicker();
  loadSheetEpisodes(sheetSeriesId);
}

function _closeEpisodePagePicker() {
  document.getElementById('episode-page-picker')?.classList.add('hidden');
  document.getElementById('episode-page-picker-btn')?.setAttribute('aria-expanded', 'false');
}

function sheetEpisodePrevPage() {
  if (_sheetEpisodePage <= 0) return;
  _sheetEpisodePage--;
  loadSheetEpisodes(sheetSeriesId);
}

function sheetEpisodeNextPage() {
  const totalPages = Math.ceil(sheetEpisodesTotal / EPISODE_PAGE_SIZE);
  if (_sheetEpisodePage + 1 >= totalPages) return;
  _sheetEpisodePage++;
  loadSheetEpisodes(sheetSeriesId);
}

function toggleEpisodeSort() {
  _sheetEpisodeSort = _sheetEpisodeSort === 'asc' ? 'desc' : 'asc';
  _sheetEpisodePage = 0;
  loadSheetEpisodes(sheetSeriesId);
}

function sheetEpisodeHtml(ep, absoluteIndex) {
  const num = ep.IndexNumber || (absoluteIndex + 1);
  const title = ep.Name || tr('episode', { num });
  const sId = ep.SeriesId || sheetSeriesId;
  const snId = ep.SeasonId || '';
  const seriesTitle = ep.SeriesName || currentSeriesTitle || '';
  const episodeLabel = episodeLabelForItem(ep) || tr('episode', { num });
  const posterItemId = sId || ep.Id;
  const mins = ep.RunTimeTicks ? `${fmtRuntime(ep.RunTimeTicks)}` : '';
  const pct = Math.round(((ep.UserData || {}).PlayedPercentage) || 0);
  const watched = pct >= 95 ? `<span class="episode-chip">${tr('watched')}</span>` : '';
  const progress = pct > 0 && pct < 95
    ? `<div class="resume-progress"><div class="resume-progress-fill" style="width:${pct}%"></div></div>`
    : '';
  const isPlaying = ep.Id === currentItemId;
  return `
    <button class="sheet-episode ${isPlaying ? 'playing' : ''}"
            data-item-id="${ep.Id}"
            data-episode-num="${num}"
            onclick="playItem('${ep.Id}', '${sId}', '${snId}', '${jsStr(title)}', '${jsStr(seriesTitle)}', '${jsStr(episodeLabel)}', '${jsStr(posterItemId)}')">
      <span class="sheet-episode-num">${tr('episodeCompact', { num })}</span>
      <span class="sheet-episode-body">
        <span class="sheet-episode-title">${esc(title)}</span>
        <span class="sheet-episode-meta">${esc([episodeLabel, mins].filter(Boolean).join(' · '))}${watched}</span>
        ${progress}
      </span>
    </button>`;
}

/* ── Global search ────────────────────────────────────────────────────────── */
function getSearchInput() {
  return document.getElementById('search-input');
}

function getSearchValue() {
  return (getSearchInput()?.textContent || '').replace(/\s+/g, ' ').trim();
}

function setSearchValue(value) {
  const input = getSearchInput();
  if (input) input.textContent = value || '';
}

function isMobileSafari() {
  const ua = navigator.userAgent || '';
  return /iP(ad|hone|od)/.test(ua) && /Safari/.test(ua) && !/CriOS|FxiOS|EdgiOS/.test(ua);
}

function activateSearch() {
  if (_activeTab !== 'library') return;
  isSearching = true;
  _setViewMode('search');
  renderSearchPrompt();
  if (!isMobileSafari()) {
    requestAnimationFrame(() => getSearchInput()?.focus());
  }
}

function onSearchInput(value) {
  if (searchInFlight) searchRequestSeq++;
  searchInFlight = false;
  if (!value.trim()) {
    renderSearchPrompt();
    return;
  }
  renderSearchPrompt(value.trim());
}

function onSearchKeydown(event) {
  if (event.key === 'Enter') {
    event.preventDefault();
    submitSearch();
  } else if (event.key === 'Escape') {
    event.preventDefault();
    cancelSearch();
  }
}

function enterSearchMode(opts = {}) {
  isSearching = true;
  if (_viewMode !== 'search') _setViewMode('search');
  if (opts.focus && !isMobileSafari()) {
    requestAnimationFrame(() => getSearchInput()?.focus());
  }
}

async function submitSearch() {
  const value = getSearchValue();
  if (!value) {
    enterSearchMode({ focus: true });
    return;
  }

  const requestId = ++searchRequestSeq;
  isSearching = true;
  searchInFlight = true;
  _setViewMode('search');
  renderSearchLoading(value);

  try {
    const data = await api('GET', `/api/emby/items?search=${encodeURIComponent(value)}&limit=60`);
    if (requestId !== searchRequestSeq || !isSearching) return;
    _lastSearchItems = data.Items || [];
    _lastSearchQuery = value;
    renderSearchResultsGrouped();
  } catch (e) {
    if (requestId !== searchRequestSeq || !isSearching) return;
    renderSearchError(e.message);
  } finally {
    if (requestId === searchRequestSeq) searchInFlight = false;
  }
}

function cancelSearch() {
  resetSearchUi();
  if (browseParentId) {
    _setViewMode('browse');
  } else {
    _setViewMode('home');
  }
}

function resetSearchUi() {
  searchRequestSeq++;
  setSearchValue('');
  isSearching = false;
  searchInFlight = false;
  _lastSearchItems = [];
  _lastSearchQuery = '';
  _searchFilter = 'all';
  updateSearchFilterPills();
}

function setSearchFilter(filter) {
  _searchFilter = filter;
  updateSearchFilterPills();
  if (_lastSearchQuery) renderSearchResultsGrouped();
  else renderSearchPrompt();
}

function updateSearchFilterPills() {
  document.querySelectorAll('.filter-pill').forEach(pill => {
    pill.classList.toggle('active', pill.dataset.filter === _searchFilter);
  });
}

function updateSearchUi() {
  // Legacy compatibility shim — no-op in new layout
}

function renderSearchLoading(value) {
  const container = document.getElementById('search-results-container');
  if (!container) return;
  container.innerHTML = `
    <div class="search-prompt-state">
      <div class="spinner" style="margin:0 auto 14px" aria-hidden="true"></div>
      <div class="search-empty-title">${tr('searchingFor', { value: esc(value) })}</div>
      <div class="search-empty-sub">${tr('searchSlowHint')}</div>
    </div>`;
}

function renderSearchPrompt(value = '') {
  const container = document.getElementById('search-results-container');
  if (!container) return;
  container.innerHTML = `
    <div class="search-prompt-state">
      <div class="search-empty-title">${value ? tr('searchFor', { value: esc(value) }) : tr('searchMedia')}</div>
      <div class="search-empty-sub">${tr('searchHint')}</div>
    </div>`;
}

function renderSearchResultsGrouped() {
  const container = document.getElementById('search-results-container');
  if (!container) return;

  const allItems = _lastSearchItems;
  if (!allItems.length) {
    container.innerHTML = `
      <div class="search-empty-state">
        <div class="search-empty-title">${tr('noSearchResults')}</div>
        <div class="search-empty-sub">${tr('tryAnotherKeyword')}</div>
      </div>`;
    return;
  }

  // Group by Type
  const groups = [
    { type: 'Movie',  label: tr('movie'), items: allItems.filter(i => i.Type === 'Movie') },
    { type: 'Series', label: tr('series'), items: allItems.filter(i => i.Type === 'Series') },
    { type: 'BoxSet', label: tr('boxset'), items: allItems.filter(i => i.Type === 'BoxSet') },
    { type: 'other',  label: tr('other'), items: allItems.filter(i => !['Movie','Series','BoxSet'].includes(i.Type)) },
  ].filter(g => g.items.length);

  const html = groups.map(g => {
    const show = _searchFilter === 'all' || _searchFilter === g.type;
    return `
      <section class="home-section search-type-section" style="${show ? '' : 'display:none'}" data-type="${g.type}">
        <div class="section-header">
          <h2 class="section-title-h">${esc(g.label)}</h2>
        </div>
        <div class="poster-row-4">
          ${g.items.slice(0, 8).map(posterCardHtml).join('')}
        </div>
      </section>`;
  }).join('');

  container.innerHTML = html || `
    <div class="search-empty-state">
      <div class="search-empty-title">${tr('noSearchResults')}</div>
      <div class="search-empty-sub">${tr('tryAnotherKeyword')}</div>
    </div>`;
}

function renderSearchError(message) {
  const container = document.getElementById('search-results-container');
  if (!container) return;
  container.innerHTML = `
    <div class="search-empty-state">
      <div class="search-empty-title">${tr('searchFailed')}</div>
      <div class="search-empty-sub">${esc(message || tr('retryLater'))}</div>
    </div>`;
}

/* ── Settings ─────────────────────────────────────────────────────────────── */
async function loadSettings() {
  renderSettingsUi();
  try {
    const settings = await api('GET', '/api/settings');
    _settings = {
      mpv_cache_secs: Number(settings.mpv_cache_secs) || 300,
      seek_backward_secs: Number(settings.seek_backward_secs) || 5,
      seek_forward_secs: Number(settings.seek_forward_secs) || 30,
    };
    renderSettingsUi();
  } catch (_) {}
}

function renderSettingsUi() {
  document.querySelectorAll('#language-segments .segment-btn').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.lang === (window.I18N?.lang || 'zh-CN'));
  });
  const minutes = Math.round((_settings.mpv_cache_secs || 300) / 60);
  const input = document.getElementById('cache-minutes-input');
  if (input && document.activeElement !== input) input.value = String(minutes);
  document.querySelectorAll('#cache-preset-row .cache-preset-btn').forEach(btn => {
    btn.classList.toggle('active', Number(btn.dataset.minutes) === minutes);
    btn.textContent = tr('cacheMinutes', { minutes: btn.dataset.minutes });
  });
  const saveBtn = document.getElementById('settings-save-btn');
  if (saveBtn) saveBtn.textContent = tr('saveSettings');
  _setText('cache-minutes-unit', tr('minutesUnit'));

  // Seek settings
  _renderSeekSelect('seek-backward-select', _settings.seek_backward_secs || 5);
  _renderSeekSelect('seek-forward-select', _settings.seek_forward_secs || 30);
  const seekSaveBtn = document.getElementById('seek-save-btn');
  if (seekSaveBtn) seekSaveBtn.textContent = tr('saveSettings');
  // Keep remote button labels in sync
  const backLabel = document.getElementById('seek-backward-label');
  const fwdLabel = document.getElementById('seek-forward-label');
  if (backLabel) backLabel.textContent = String(_settings.seek_backward_secs || 5);
  if (fwdLabel) fwdLabel.textContent = String(_settings.seek_forward_secs || 30);
}

function _renderSeekSelect(id, selectedValue) {
  const sel = document.getElementById(id);
  if (!sel) return;
  if (sel.options.length === 0) {
    for (let s = 5; s <= 60; s += 5) {
      const opt = document.createElement('option');
      opt.value = String(s);
      opt.textContent = tr('seekSeconds', { secs: s });
      sel.appendChild(opt);
    }
  }
  sel.value = String(selectedValue);
}

function setAppLanguage(lang) {
  window.I18N?.setLang(lang);
  window.location.reload();
}

let _settingsSaveTimer = null;

function setCacheMinutes(minutes) {
  const safe = Math.max(1, Math.min(120, Math.round(Number(minutes) || 30)));
  _settings.mpv_cache_secs = safe * 60;
  _setSettingsStatus('');
  renderSettingsUi();
  clearTimeout(_settingsSaveTimer);
  _settingsSaveTimer = setTimeout(saveSettings, 200);
}

function onCacheMinutesInput() {
  const input = document.getElementById('cache-minutes-input');
  const minutes = Math.max(1, Math.min(120, Math.round(Number(input?.value) || 30)));
  _settings.mpv_cache_secs = minutes * 60;
  _setSettingsStatus('');
  renderSettingsUi();
  clearTimeout(_settingsSaveTimer);
  _settingsSaveTimer = setTimeout(saveSettings, 200);
}

async function saveSettings() {
  try {
    const saved = await api('PUT', '/api/settings', {
      mpv_cache_secs: Math.max(60, Math.min(7200, Math.round(Number(_settings.mpv_cache_secs) || 300))),
    });
    _settings = { mpv_cache_secs: Number(saved.mpv_cache_secs) || _settings.mpv_cache_secs };
    renderSettingsUi();
    _setSettingsStatus(tr('settingsSaved'), false);
  } catch (e) {
    _setSettingsStatus(e.message || tr('settingsSaveFailed'), true);
  }
}

function _setSettingsStatus(message, isError = false) {
  const el = document.getElementById('settings-status');
  if (!el) return;
  el.textContent = message || '';
  el.className = 'settings-status' + (message ? (isError ? ' err' : ' ok') : '');
}

let _seekSettingsSaveTimer = null;

function onSeekSettingChange() {
  const back = Number(document.getElementById('seek-backward-select')?.value) || 5;
  const fwd = Number(document.getElementById('seek-forward-select')?.value) || 30;
  _settings.seek_backward_secs = back;
  _settings.seek_forward_secs = fwd;
  _setSeekSettingsStatus('');
  renderSettingsUi();
  clearTimeout(_seekSettingsSaveTimer);
  _seekSettingsSaveTimer = setTimeout(saveSeekSettings, 200);
}

async function saveSeekSettings() {
  try {
    const saved = await api('PUT', '/api/settings', {
      seek_backward_secs: _settings.seek_backward_secs || 5,
      seek_forward_secs: _settings.seek_forward_secs || 30,
    });
    _settings.seek_backward_secs = Number(saved.seek_backward_secs) || _settings.seek_backward_secs;
    _settings.seek_forward_secs = Number(saved.seek_forward_secs) || _settings.seek_forward_secs;
    renderSettingsUi();
    _setSeekSettingsStatus(tr('settingsSaved'), false);
  } catch (e) {
    _setSeekSettingsStatus(e.message || tr('settingsSaveFailed'), true);
  }
}

function _setSeekSettingsStatus(message, isError = false) {
  const el = document.getElementById('seek-settings-status');
  if (!el) return;
  el.textContent = message || '';
  el.className = 'settings-status' + (message ? (isError ? ' err' : ' ok') : '');
}

/* ── Settings: server CRUD ────────────────────────────────────────────────── */
function openServerManager() {
  document.getElementById('server-manager-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
  refreshServerSwitcher();
}

function closeServerManager() {
  document.getElementById('server-manager-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
}

function onServerManagerBackdropClick(event) {
  if (event.target.id === 'server-manager-backdrop') closeServerManager();
}

function renderServerList(servers) {
  const el = document.getElementById('server-list');
  if (!el) return;
  if (!servers.length) {
    el.innerHTML = `<div class="empty-hint" style="padding:20px 0;text-align:left">${tr('noServerHint')}</div>`;
    return;
  }
  el.innerHTML = servers.map(s => {
    const isFile   = _normType(s.type) === 'file';
    const hosts    = s.hosts || [];
    const activeH  = s.active_host || 0;
    const hostsHtml = isFile ? '' : hosts.map((h, i) => `
      <div class="host-row ${i === activeH ? 'active-host' : ''}"
           onclick="switchServerHost(event, '${s.id}', ${i}, ${s.active})">
        <div class="host-dot ${i === activeH ? 'active' : ''}"></div>
        <div class="host-addr">${esc(h)}:${s.port}</div>
        ${i === activeH ? `<div class="host-tag">${tr('current')}</div>` : (i === 0 ? `<div class="host-tag" style="color:var(--text-muted)">${tr('primary')}</div>` : `<div class="host-tag" style="color:var(--text-muted)">${tr('secondary')}</div>`)}
      </div>`).join('');
    const statusHtml = isFile
      ? `<span class="server-status file-meta">${esc(_sourceMeta(s))}</span>`
      : `<span class="server-status ${s.logged_in ? 'logged-in' : 'not-logged'}">${s.logged_in ? tr('loggedIn') : tr('notLoggedIn')}</span>`;
    return `
      <div class="server-card ${s.active ? 'active-server' : ''}">
        <div class="server-card-header">
        <div class="server-card-name">${esc(s.name || tr('server'))} ${_sourceBadge(s.type)}</div>
          ${statusHtml}
          ${s.active ? `<span class="server-status active-badge">${tr('current')}</span>` : ''}
        </div>
        ${hostsHtml ? `<div class="server-card-hosts">${hostsHtml}</div>` : ''}
        <div class="server-card-actions">
          ${!s.active ? `<button class="btn-sm primary" onclick="switchServer('${s.id}', '${jsStr(s.name || tr('server'))}')">${tr('switchHere')}</button>` : ''}
          <button class="btn-sm" onclick="openServerForm('${s.id}')">${tr('edit')}</button>
          <button class="btn-sm danger" onclick="confirmDeleteServer('${s.id}', '${jsStr(s.name || tr('server'))}')">${tr('delete')}</button>
        </div>
      </div>`;
  }).join('');
}

function openServerForm(serverId) {
  closeServerManager();
  document.getElementById('server-form-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
  const form = document.getElementById('server-form');
  const title = document.getElementById('server-form-title');
  if (title) title.textContent = serverId ? tr('editServer') : tr('addServer');

  document.getElementById('form-server-id').value = serverId || '';
  document.getElementById('form-status').textContent = '';
  _renderFileProtocolOptions('local');

  const clearForm = () => {
    document.getElementById('form-name').value     = '';
    document.querySelectorAll('input[name="proto"]').forEach(r => { r.checked = r.value === 'http'; });
    ['form-host0','form-host1','form-host2','form-host3','form-host4'].forEach(id => { document.getElementById(id).value = ''; });
    document.getElementById('form-port').value     = 8096;
    document.getElementById('form-username').value = '';
    document.getElementById('form-password').value = '';
    document.getElementById('form-token').value    = '';
    document.getElementById('form-root').value     = '';
  };

  clearForm();
  if (serverId) {
    // Populate from existing server
    api('GET', '/api/servers').then(servers => {
      const s = servers.find(x => x.id === serverId);
      if (!s) { setServerFormType('emby'); return; }
      document.getElementById('form-name').value     = s.name || '';
      document.querySelectorAll('input[name="proto"]').forEach(r => { r.checked = r.value === (s.protocol || 'http'); });
      const hosts = s.hosts || [];
      ['form-host0','form-host1','form-host2','form-host3','form-host4'].forEach((id, i) => {
        document.getElementById(id).value = hosts[i] || '';
      });
      document.getElementById('form-port').value     = s.port || 8096;
      document.getElementById('form-username').value = s.username || '';
      document.getElementById('form-password').value = '';
      _renderFileProtocolOptions(s.file_protocol || 'local');
      document.getElementById('form-root').value     = s.root || '';
      setServerFormType(_normType(s.type));
    });
  } else {
    setServerFormType('emby');
  }

  form.scrollIntoView({ behavior: 'smooth', block: 'start' });
}

function closeServerForm() {
  document.getElementById('server-form-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
  openServerManager();
}

function closeServerFormOnly() {
  document.getElementById('server-form-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
}

async function _onSaveSuccess(id) {
  // Activate if this is the first / only server.
  const servers = await api('GET', '/api/servers');
  if (id && !servers.find(s => s.active)) {
    await api('POST', `/api/servers/${id}/activate`);
  }
  await refreshServerSwitcher();
  // Close the form (without reopening the server manager) and go straight
  // to the library so the user immediately sees their content.
  closeServerFormOnly();
  switchTab('library');
  await reloadLibraryForActiveServer();
}

function onServerFormBackdropClick(event) {
  if (event.target.id === 'server-form-backdrop') closeServerForm();
}

async function saveAndLogin() {
  const statusEl = document.getElementById('form-status');
  const serverId = document.getElementById('form-server-id').value;
  const type     = _serverFormType;
  const name     = document.getElementById('form-name').value.trim();
  const username = document.getElementById('form-username').value.trim();
  const password = document.getElementById('form-password').value;
  const setStatus = (msg, cls = '') => { statusEl.textContent = msg; statusEl.className = 'form-status' + (cls ? ' ' + cls : ''); };

  // ── File source ──────────────────────────────────────────────────────────
  if (type === 'file') {
    const fileProtocol = document.getElementById('form-file-protocol').value || 'local';
    const root = document.getElementById('form-root').value.trim();
    if (!root) { setStatus(tr('needRoot'), 'err'); return; }
    setStatus(tr('testingConnection'));
    try {
      let id = serverId;
      if (serverId) {
        const body = { name, type, file_protocol: fileProtocol, root, username };
        if (password) body.password = password;   // blank → keep existing
        await api('PUT', `/api/servers/${serverId}`, body);
        await api('POST', `/api/servers/${serverId}/login`, {});  // verify reachability
      } else {
        const srv = await api('POST', '/api/servers', { name, type, file_protocol: fileProtocol, root, username, password });
        id = srv.id;
      }
      setStatus(tr('saved'), 'ok');
      await _onSaveSuccess(id);
    } catch (e) { setStatus(e.message, 'err'); }
    return;
  }

  // ── Media server (emby / jellyfin / plex) ─────────────────────────────────
  const protocol = document.querySelector('input[name="proto"]:checked')?.value || 'http';
  const hosts    = ['form-host0','form-host1','form-host2','form-host3','form-host4']
    .map(id => document.getElementById(id).value.trim()).filter(Boolean);
  const port     = parseInt(document.getElementById('form-port').value, 10) || (type === 'plex' ? 32400 : 8096);
  const token    = document.getElementById('form-token').value.trim();
  const hasCreds = (username && password) || (type === 'plex' && token);

  if (!hosts.length){ setStatus(tr('needOneHost'), 'err'); return; }
  setStatus(tr('saving'));

  try {
    let id = serverId;
    if (serverId) {
      // Editing an existing (already-saved) server: update fields, then log in.
      await api('PUT', `/api/servers/${serverId}`, { name, type, protocol, hosts, port, username });
      if (hasCreds) {
        setStatus(tr('loggingIn'));
        await api('POST', `/api/servers/${serverId}/login`, { username, password, token });
        setStatus(tr('loginSuccess'), 'ok');
      } else {
        setStatus(tr('savedNotLoggedIn'), 'ok');
      }
    } else {
      // Adding a new server: the backend only persists it if the login
      // succeeds, so a wrong address/password leaves nothing behind.
      if (hasCreds) setStatus(tr('connectingAndLoggingIn'));
      const srv = await api('POST', '/api/servers', { name, type, protocol, hosts, port, username, password, token });
      id = srv.id;
      setStatus(hasCreds ? tr('loginSuccess') : tr('savedNotLoggedIn'), 'ok');
    }
    await _onSaveSuccess(id);
  } catch (e) {
    setStatus(e.message, 'err');
  }
}

async function confirmDeleteServer(serverId, name) {
  if (!confirm(tr('confirmDeleteServer', { name }))) return;
  try {
    await api('DELETE', `/api/servers/${serverId}`);
    await refreshServerSwitcher();
  } catch (e) { toast(e.message, true); }
}

/* ── Server form: type & file fields ──────────────────────────────────────── */
function setServerFormType(type) {
  _serverFormType = _normType(type);
  document.querySelectorAll('#form-type-seg .type-seg-btn').forEach(b =>
    b.classList.toggle('active', b.dataset.type === _serverFormType));
  const isFile = _serverFormType === 'file';
  document.getElementById('form-group-server')?.classList.toggle('hidden', isFile);
  document.getElementById('form-group-file')?.classList.toggle('hidden', !isFile);
  document.getElementById('form-row-token')?.classList.toggle('hidden', _serverFormType !== 'plex');

  // Default port per media-server type (preserve a custom port).
  const portInput = document.getElementById('form-port');
  if (portInput && !isFile) {
    const cur = portInput.value;
    if (!cur || cur === '8096' || cur === '32400') portInput.value = (_serverFormType === 'plex') ? '32400' : '8096';
  }

  _setText('form-cred-hint', isFile ? tr('shareCredHint') : (_serverFormType === 'plex' ? tr('plexAccountHint') : tr('accountHint')));
  _setText('form-token-hint', tr('plexTokenHint'));
  _updateRootHint();

  const btn = document.getElementById('form-submit-btn');
  if (btn) btn.textContent = isFile ? tr('save') : tr('saveAndLogin');
}

function _renderFileProtocolOptions(selected = 'local') {
  const sel = document.getElementById('form-file-protocol');
  if (!sel) return;
  const opts = [
    ['local', tr('protoLocalOpt')],
    ['smb', tr('protoSmbOpt')],
    ['webdav', 'WebDAV'],
    ['ftp', 'FTP'],
    ['sftp', 'SFTP'],
    ['nfs', tr('protoNfsOpt')],
  ];
  sel.innerHTML = opts.map(([v, l]) => `<option value="${v}"${v === selected ? ' selected' : ''}>${esc(l)}</option>`).join('');
}

function onFileProtocolChange() { _updateRootHint(); }

function _updateRootHint() {
  const proto = document.getElementById('form-file-protocol')?.value || 'local';
  const isLocal = proto === 'local';
  const hintText = isLocal ? tr('rootHintLocal') : tr('rootHintNet');
  _setText('form-root-hint', hintText);
  const rootInput = document.getElementById('form-root');
  if (rootInput) rootInput.placeholder = hintText;
}

/* ── File browser (file sources) ──────────────────────────────────────────── */
function _enterFileMode() {
  switchTab('library');
  document.getElementById('search-toggle')?.classList.add('hidden');
  _setViewMode('files');
}

function _exitFileMode() {
  document.getElementById('search-toggle')?.classList.toggle('hidden', _activeTab !== 'library');
}

async function loadDir(path = '') {
  _filesPath = path;
  _enterFileMode();
  const list = document.getElementById('files-list');
  if (list) list.innerHTML = `<div class="files-loading">${tr('loadingFolder')}</div>`;
  try {
    const data = await api('GET', `/api/files/list?path=${encodeURIComponent(path)}`);
    renderFileBrowser(data);
  } catch (e) {
    document.getElementById('files-breadcrumb').innerHTML = '';
    if (list) list.innerHTML = `
      <div class="files-error">
        <div class="files-error-title">${tr('folderFailed')}</div>
        <div class="files-error-sub">${esc(e.message)}</div>
      </div>`;
  }
}

function renderFileBrowser(data) {
  _filesPath = data.path || '';
  renderBreadcrumb(data.breadcrumb || []);
  const list = document.getElementById('files-list');
  if (!list) return;
  const entries = data.entries || [];
  if (!entries.length) {
    list.innerHTML = `<div class="files-empty">${tr('emptyFolder')}</div>`;
    return;
  }
  list.innerHTML = entries.map(fileRowHtml).join('');
}

function renderBreadcrumb(crumbs) {
  const el = document.getElementById('files-breadcrumb');
  if (!el) return;
  el.innerHTML = crumbs.map((c, i) => {
    const last = i === crumbs.length - 1;
    return `<button class="crumb${last ? ' active' : ''}" onclick="loadDir('${jsStr(c.path)}')">${esc(c.name)}</button>`
      + (last ? '' : '<span class="crumb-sep">›</span>');
  }).join('');
}

const _FOLDER_ICON = '<svg class="file-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>';
const _VIDEO_ICON = '<svg class="file-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="4" width="20" height="16" rx="2.5"/><path d="m10 9 5 3-5 3z" fill="currentColor" stroke="none"/></svg>';
const _CHEVRON_ICON = '<svg class="file-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>';

function fileRowHtml(e) {
  if (e.is_dir) {
    return `<button class="file-row file-dir" onclick="loadDir('${jsStr(e.path)}')">
      ${_FOLDER_ICON}
      <span class="file-name">${esc(e.name)}</span>
      ${_CHEVRON_ICON}
    </button>`;
  }
  return `<button class="file-row file-video" onclick="playFile('${jsStr(e.path)}', '${jsStr(e.name)}')">
    ${_VIDEO_ICON}
    <span class="file-name">${esc(e.name)}</span>
    <span class="file-size">${fmtSize(e.size)}</span>
  </button>`;
}

function fmtSize(n) {
  n = Number(n) || 0;
  if (n <= 0) return '';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return (i === 0 ? n : n.toFixed(1)) + ' ' + units[i];
}

async function playFile(path, name) {
  _hadProps = false;
  _currentAspect = 'fit';
  _loopFile = false;
  currentItemId = '';
  currentSeriesId = '';
  currentSeasonId = '';
  currentSeriesTitle = '';
  currentEpisodeLabel = '';
  currentPosterItemId = '';
  currentItemIsSeries = false;
  setNowPlaying(name, {});
  switchTab('remote');
  document.getElementById('play-loading-tag')?.classList.remove('hidden');
  document.getElementById('player-info-placeholder')?.classList.remove('hidden');
  document.getElementById('player-info')?.classList.add('hidden');
  try {
    await api('POST', '/api/player/play', { path, title: name });
    _fetchAndUpdateProps();
  } catch (e) {
    toast(e.message, true);
    setNowPlaying('');
  } finally {
    document.getElementById('play-loading-tag')?.classList.add('hidden');
  }
}

/* ── Helpers ──────────────────────────────────────────────────────────────── */
function _truncate(str, maxLen = 8) {
  if (!str) return '';
  return str.length > maxLen ? str.slice(0, maxLen) + '…' : str;
}

function esc(str) {
  return String(str || '').replace(/[&<>"']/g, c =>
    ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;' }[c]));
}

// Use inside single-quoted JS strings within onclick="..." attributes.
// The HTML parser runs before JS, so &#39; would decode back to ' and break
// the string literal — backslash-escaping is correct here instead.
function jsStr(str) {
  return String(str || '')
    .replace(/\\/g, '\\\\')
    .replace(/'/g, "\\'")
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/\r?\n|\r/g, ' ');
}

/* ── Player engine picker ─────────────────────────────────────────────────── */

let _currentEngine = 'mpv';

async function fetchEngine() {
  try {
    const d = await api('GET', '/api/desktop/engine');
    if (d.platform !== 'darwin') return; // only show on macOS
    _currentEngine = d.engine || 'mpv';
    _updateEngineUI();
    document.getElementById('engine-hint').classList.remove('hidden');
  } catch (_) {}
}

function _updateEngineUI() {
  const checkMpv = document.getElementById('engine-check-mpv');
  const checkAv  = document.getElementById('engine-check-avplayer');
  const rowMpv   = document.getElementById('engine-opt-mpv');
  const rowAv    = document.getElementById('engine-opt-avplayer');
  if (!checkMpv) return;
  if (_currentEngine === 'avplayer') {
    checkMpv.classList.add('hidden');
    checkAv.classList.remove('hidden');
    rowMpv.classList.remove('active');
    rowAv.classList.add('active');
  } else {
    checkMpv.classList.remove('hidden');
    checkAv.classList.add('hidden');
    rowMpv.classList.add('active');
    rowAv.classList.remove('active');
  }
}

async function setEngine(engine) {
  try {
    const d = await api('POST', '/api/desktop/engine', { engine });
    _currentEngine = d.engine || 'mpv';
    _updateEngineUI();
  } catch (e) {
    toast(e.message || tr('engineSwitchFailed'), true);
  }
}

function toast(msg, isError = false) {
  const el = Object.assign(document.createElement('div'), {
    textContent: msg,
    style: `
      position:fixed;bottom:80px;left:50%;transform:translateX(-50%);
      background:${isError ? 'var(--danger)' : 'var(--accent)'};
      color:#fff;padding:10px 18px;border-radius:20px;font-size:13px;
      z-index:999;white-space:nowrap;pointer-events:none;
      animation:fadeUp .25s ease`,
  });
  document.body.appendChild(el);
  setTimeout(() => el.remove(), 2800);
}
