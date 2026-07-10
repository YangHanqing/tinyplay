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
let browseMode = 'library'; // 'library' | 'resume' — what loadBrowse() fetches into #poster-grid
let browseStart = 0;
const PAGE_SIZE = 60;
const RESUME_PAGE_SIZE = 30;
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
let _sheetPlayableItem = null;
let _sheetMediaSourceId = '';
let _locateEpisode = null;  // { id, num } to scroll to when opening a series sheet
let sheetEpisodesTotal = 0;
let sheetEpisodesLoading = false;
let _sheetEpisodePage = 0;
let _sheetEpisodeSort = 'asc';
let sheetSeasons = [];      // [] when the source has one season or the seasons API isn't available
let sheetSeasonId = '';

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
let _settings = { mpv_cache_secs: 300, seek_backward_secs: 5, seek_forward_secs: 30, dlna_receiver_enabled: true };
// null = unknown/unrestricted (Python branch never sends this field, and it
// implements every protocol); an array restricts the file-source dropdown to
// what this backend can actually browse (see desktop-go's config.Settings()).
let _supportedFileProtocols = null;
let _serviceOnline = true;
let _serviceProbeInFlight = false;

// Active source type drives whether the library tab shows the poster wall
// (emby/jellyfin/plex) or the file browser (webdav/smb/local/nfs). Set by
// refreshServerSwitcher.
let _activeSourceType = 'emby';
let _activeServerId = '';
let _playbackServerId = '';
let _serverSwitchSeq = 0;
const _serverSwitchSession = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random()}`;
let _libraryGeneration = 0;
let _libraryAbortController = new AbortController();
// Whether any server/source is configured at all. Set by refreshServerSwitcher;
// starts true so the search icon isn't hidden for a flash before the first load.
let _hasAnyServer = true;
let _filesPath = '';            // current folder path in file mode

/* ── IPTV state ───────────────────────────────────────────────────────────── */
let _iptvCategory = 'all';
let _iptvSearch = '';
let _iptvChannels = [];         // last-loaded channel rows for the active category/search
let currentItemIsLive = false;  // true while an IPTV channel is the now-playing item
let currentPlaybackSourceType = 'emby';
let currentIPTVChannelId = '';
let currentIPTVVariantIndex = 0;
let currentIPTVVariantCount = 0;
let currentIPTVHasProgramme = false;
let _iptvQualityOpen = false;
let _iptvProgrammeOpen = false;
let _iptvResumeWatchdog = null;

const ASPECT_OPTIONS = [
  { value: 'fit',      labelKey: 'aspectFitLabel',      displayKey: 'aspectFitDisplay' },
  { value: 'zoom',     labelKey: 'aspectZoomLabel',     displayKey: 'aspectZoomDisplay' },
  { value: 'stretch',  labelKey: 'aspectStretchLabel',  displayKey: 'aspectStretchDisplay' },
  { value: 'original', labelKey: 'aspectOriginalLabel', displayKey: 'aspectOriginalDisplay' },
];

function aspectLabel(opt) { return tr(opt.labelKey); }
function aspectDisplay(opt) { return tr(opt.displayKey); }

/* ── Platform detection ──────────────────────────────────────────────────── */
function detectPlatformAndPatchI18n() {
  fetch('/desktop', { method: 'HEAD', cache: 'no-store' }).then(r => {
    if (r.ok) patchServiceOfflineForDesktop();
  }).catch(() => {});
}

function patchServiceOfflineForDesktop() {
  const zh = window.I18N?.t;
  if (!zh) return;
  // Override the key lookup so tr('serviceOfflineBody') returns desktop text.
  const origT = I18N.t;
  I18N.t = (key, params) => {
    if (key === 'serviceOfflineTitle') {
      return window.I18N.isZh() ? 'TinyPlay 服务不在线' : 'TinyPlay is offline';
    }
    if (key === 'serviceOfflineBody') {
      return window.I18N.isZh()
        ? '手机页面还在，但现在连不上桌面端服务。请确认 TinyPlay 正在运行。'
        : 'The remote page is still here, but it cannot reach TinyPlay. Make sure TinyPlay is running on your computer.';
    }
    return origT(key, params);
  };
}

/* ── Boot ─────────────────────────────────────────────────────────────────── */
document.addEventListener('DOMContentLoaded', async () => {
  document.addEventListener('click', onDocClick);
  registerPWA();
  detectPlatformAndPatchI18n();
  setupServiceReachability();

  await refreshServerSwitcher();
  await restorePlayerContext();
  _setViewMode('home');
  startPropPolling();
  _fetchAndUpdateProps();
  await loadActiveSource();
  await loadSettings();
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
    </div>`;
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
  // An unconfigured installation has a dedicated empty state. Do not probe the
  // library API in that case: its expected "no active server" response is not
  // an actionable error and must not become a first-launch toast.
  if (!_hasAnyServer || !_activeServerId) {
    resetLibraryState();
    updateLibraryEmptyState(true);
    return;
  }
  updateLibraryEmptyState(false);
  if (isFileSourceType(_activeSourceType)) {
    _enterFileMode();
    await loadDir('');
    return;
  }
  if (_activeSourceType === 'iptv') {
    await _enterIPTVMode();
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
      _playbackServerId = state.server_id || _activeServerId;
      currentPlaybackSourceType = state.source_type || 'emby';
      currentSeriesId  = state.series_id || '';
      currentSeasonId  = state.season_id || '';
      currentSeriesTitle = state.series_title || '';
      currentEpisodeLabel = state.episode_label || '';
      currentPosterItemId = state.source_type === 'dlna' || state.is_live ? '' : (state.poster_item_id || state.series_id || state.item_id || '');
      currentItemId    = state.item_id || '';
      currentItemIsSeries = !!currentSeriesId;
      currentIPTVChannelId = state.is_live ? (state.channel_id || '') : '';
      currentIPTVVariantIndex = 0;
      currentIPTVVariantCount = 0;
      setNowPlaying(state.title, {
        seriesTitle: currentSeriesTitle,
        episodeLabel: currentEpisodeLabel,
        posterItemId: currentPosterItemId,
        isLive: !!state.is_live,
      });
      if (state.is_live && currentIPTVChannelId) {
        // variant_count isn't in /api/player/state; fetch it once so the
        // quality tile's visibility (shown only when >1 variant) is correct
        // right after a browser reconnect, not just after a fresh play().
        api('GET', `/api/iptv/channel/${encodeURIComponent(currentIPTVChannelId)}?server_id=${encodeURIComponent(_playbackServerId)}`).then(ch => {
          currentIPTVVariantCount = (ch.variants || []).length;
          currentIPTVHasProgramme = iptvChannelHasProgramme(ch);
          updateLiveControlsVisibility(currentItemIsLive);
        }).catch(() => {});
      }
    }
  } catch (_) { /* backend may not be ready yet */ }
}

/* ── API helper ───────────────────────────────────────────────────────────── */
async function api(method, path, body, extra = {}) {
  if (method === 'GET' && path.startsWith('/api/library/') && _activeServerId && !/[?&]server_id=/.test(path)) {
    path += `${path.includes('?') ? '&' : '?'}server_id=${encodeURIComponent(_activeServerId)}`;
    if (!extra.signal) extra.signal = _libraryAbortController.signal;
  }
  const opts = { method, headers: { 'Content-Type': 'application/json' } };
  if (extra.signal) opts.signal = extra.signal;
  if (body !== undefined) opts.body = JSON.stringify(body);
  let r;
  try {
    r = await fetch(path, opts);
  } catch (error) {
    if (error?.name === 'AbortError') throw error;
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

function beginLibraryLoad(serverId = _activeServerId) {
  _libraryAbortController.abort();
  _libraryAbortController = new AbortController();
  return { generation: ++_libraryGeneration, serverId, signal: _libraryAbortController.signal };
}

function currentLibraryLoad() {
  return { generation: _libraryGeneration, serverId: _activeServerId, signal: _libraryAbortController.signal };
}

function isCurrentLibraryLoad(load) {
  return load && load.generation === _libraryGeneration && load.serverId === _activeServerId;
}

function libraryApi(method, path, body, load = currentLibraryLoad()) {
  let bound = path;
  if (load.serverId) bound += `${bound.includes('?') ? '&' : '?'}server_id=${encodeURIComponent(load.serverId)}`;
  return api(method, bound, body, { signal: load.signal });
}

function libraryImageUrl(itemId, maxHeight, type = '', serverId = _activeServerId) {
  const params = new URLSearchParams({ max_height: String(maxHeight) });
  if (type) params.set('type', type);
  if (serverId) params.set('server_id', serverId);
  return `/api/library/image/${encodeURIComponent(itemId)}?${params}`;
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
    currentPlaybackSourceType = 'emby';
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

async function playItem(itemId, seriesId, seasonId, title, seriesTitle = '', episodeLabel = '', posterItemId = '', mediaSourceId = '', serverId = _activeServerId) {
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
  currentPlaybackSourceType = _activeSourceType;
  currentIPTVChannelId = '';
  currentIPTVVariantIndex = 0;
  currentIPTVVariantCount = 0;
  currentIPTVHasProgramme = false;
  _playbackServerId = serverId;
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
      server_id: _playbackServerId,
      item_id: itemId,
      series_id: currentSeriesId,
      season_id: currentSeasonId,
      title: title || '',
      series_title: currentSeriesTitle,
      episode_label: currentEpisodeLabel,
      poster_item_id: resolvedPoster,
      media_source_id: mediaSourceId || '',
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
      server_id: _playbackServerId,
      series_id: seriesId,
      season_id: seasonId || '',
      start: 0,
      limit: 200,
      sort: 'asc',
    });
    const data = await api('GET', `/api/library/episodes?${qs}`);
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
    '',
    _playbackServerId,
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
      nowPlaying.style.setProperty('--now-poster', `url("${libraryImageUrl(posterItemId, 700, 'Backdrop', _playbackServerId)}")`);
      nowPlaying.classList.add('has-poster');
      if (posterImg) {
        posterImg.src = libraryImageUrl(posterItemId, 520, '', _playbackServerId);
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
  currentItemIsLive = !!meta.isLive;
  updateEpisodeNavVisibility(currentItemIsSeries);
  updateLiveControlsVisibility(currentItemIsLive);
  if (posterItemId && hasTitle) {
    applyPosterColor(libraryImageUrl(posterItemId, 80, '', _playbackServerId));
  } else if (!hasTitle) {
    resetPosterColor();
  }
}

function updateEpisodeNavVisibility(isSeries) {
  document.getElementById('ep-controls')?.classList.toggle('hidden', !isSeries);
  const movieInfo = document.getElementById('now-playing-movie-info');
  if (movieInfo) {
    movieInfo.classList.toggle('hidden', isSeries);
    if (!isSeries) movieInfo.textContent = isCurrentIPTVPlayback() ? sourceTypeLabel('iptv') : (isDLNAPlayback() ? tr('dlnaReceiver') : tr('movie'));
  }
}

function isCurrentIPTVPlayback() {
  return currentItemIsLive || !!currentIPTVChannelId;
}

function isDLNAPlayback() { return currentPlaybackSourceType === 'dlna'; }

// Live channels have no seek bar/duration (nothing to scrub through) and get
// two extra tool tiles (quality/variant switcher, programme guide) instead.
// Subtitle and speed are also hidden for live: IPTV (M3U + optional XMLTV EPG)
// has no subtitle source, and "speed" is meaningless for a live broadcast.
// Audio track switching stays — multi-language channels commonly multiplex
// alternate audio.
function updateLiveControlsVisibility(isLive) {
  const live = isLive || isCurrentIPTVPlayback();
  const dlna = isDLNAPlayback();
  setLiveProgressHidden(live);
  document.getElementById('btn-seek-backward')?.classList.toggle('hidden', live);
  document.getElementById('btn-seek-forward')?.classList.toggle('hidden', live);
  document.getElementById('tool-tile-iptv-quality')?.classList.toggle('hidden', !live || currentIPTVVariantCount <= 1);
  document.getElementById('tool-tile-iptv-guide')?.classList.toggle('hidden', !live || !currentIPTVHasProgramme);
  document.getElementById('tool-tile-iptv-live')?.classList.toggle('hidden', !live);
  // DLNA contributes the stream URL, but does not standardise runtime audio or
  // subtitle track selection. Keep those local-player controls out of its UI.
  document.getElementById('tool-tile-audio')?.classList.toggle('hidden', dlna);
  document.getElementById('tool-tile-subtitle')?.classList.toggle('hidden', live || dlna);
  document.getElementById('tool-tile-speed')?.classList.toggle('hidden', live);
  _setText('tool-tile-iptv-quality-label', tr('iptvQualitySwitcher'));
  _setText('tool-tile-iptv-guide-label', tr('iptvProgrammeGuide'));
  _setText('tool-tile-iptv-live-label', tr('iptvBackToLive'));
}

function updateLibraryEmptyState(isEmpty) {
  document.getElementById('lib-empty-state')?.classList.toggle('hidden', !isEmpty);
  document.getElementById('lib-home-view')?.classList.toggle('hidden', isEmpty);
}

/* ── View mode ────────────────────────────────────────────────────────────── */
function _setViewMode(mode) {
  _viewMode = mode;
  document.getElementById('lib-home-view')?.classList.toggle('hidden', mode !== 'home');
  document.getElementById('lib-grid-view')?.classList.toggle('hidden', !['browse', 'resume'].includes(mode));
  document.getElementById('lib-search-view')?.classList.toggle('hidden', mode !== 'search');
  document.getElementById('lib-search-area')?.classList.toggle('hidden', mode !== 'search');
  document.getElementById('library-toolbar')?.classList.toggle('hidden', mode !== 'browse');
  document.getElementById('lib-files-view')?.classList.toggle('hidden', mode !== 'files');
  document.getElementById('lib-iptv-view')?.classList.toggle('hidden', mode !== 'iptv');
  document.getElementById('btn-iptv-refresh')?.classList.toggle('hidden', mode !== 'iptv');
  document.getElementById('browse-back-btn')?.classList.toggle('hidden', mode !== 'resume');
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
  document.getElementById('search-toggle')?.classList.toggle('hidden', tab !== 'library' || isFileSourceType(_activeSourceType) || _activeSourceType === 'iptv' || !_hasAnyServer);
  document.getElementById('btn-iptv-refresh')?.classList.toggle('hidden', tab !== 'library' || _viewMode !== 'iptv');
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
let _playerStatePollTicks = 0;

function _schedulePropPoll() {
  if (!_propPolling) return;
  _propPollTimer = setTimeout(async () => {
    if (!document.hidden) {
      await _fetchAndUpdateProps();
      // A DLNA sender can replace playback without a phone-originated API
      // call. Re-read context periodically so an already-open remote switches
      // from its old library metadata to the receiver-safe DLNA layout.
      if (++_playerStatePollTicks % 5 === 0) {
        const state = await api('GET', '/api/player/state').catch(() => null);
        if (state?.running && state.source_type !== currentPlaybackSourceType) {
          await restorePlayerContext();
        }
      }
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

// core-idle stays true while mpv is loading/buffering and has not yet
// rendered the first frame; combined with the other cache signals this tells
// apart "stalled" from a cold start (isStarting, computed separately by the
// caller) or an intentional pause.
function isPlaybackStalled(p) {
  const paused = p['pause'];
  const cacheState = p['demuxer-cache-state'] || {};
  const cacheDur = Number(cacheState['fw-duration'] ?? cacheState['cache-duration']);
  const underrun = Boolean(cacheState.underrun || cacheState['cache-underrun']);
  const coreIdle = Boolean(p['core-idle']);
  const pausedForCache = Boolean(p['paused-for-cache']);
  return underrun || pausedForCache
    || (!paused && coreIdle)
    || (!paused && Number.isFinite(cacheDur) && cacheDur < 0.3);
}

function _applyProps(p) {
  _latestProps = p || {};
  const livePlayback = isCurrentIPTVPlayback();
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
  if (livePlayback) {
    setLiveProgressHidden(true);
  } else if (!_isDraggingProgress) {
    setLiveProgressHidden(false);
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
  const cacheSpeedBps = Number(p['cache-speed'] ?? 0);

  // core-idle stays true while mpv is loading/buffering and has not yet
  // rendered the first frame. Before that first frame it's a cold start
  // (starting); a stall after playback had already begun is a distinct
  // rebuffering state, matching the Apple TV OSD pill.
  const isStarting = !paused && pos === 0 && (dur === null || dur === 0);
  const isStalled = isPlaybackStalled(p);
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
  if (!livePlayback) {
    const dlSpeedText = cacheSpeedBps > 1000
      ? (cacheSpeedBps >= 1e6
          ? `↓${(cacheSpeedBps / 1e6).toFixed(1)}MB/s`
          : `↓${Math.round(cacheSpeedBps / 1e3)}KB/s`)
      : '';
    const cacheDurText = fmtCacheDur(cacheDur);
    const inlineParts = [cacheDurText, dlSpeedText].filter(Boolean);
    _setText('health-cache-speed', inlineParts.join(' '));
  }

  // Cache buffer overlay on progress bar
  const cacheFill = document.getElementById('progress-cache-fill');
  if (cacheFill && !livePlayback) {
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

  // Movie info line (duration shown when not a series; IPTV is live, no duration).
  if (!currentItemIsSeries) {
    const movieInfo = document.getElementById('now-playing-movie-info');
    if (movieInfo && !movieInfo.classList.contains('hidden')) {
      if (isCurrentIPTVPlayback()) {
        movieInfo.textContent = sourceTypeLabel('iptv');
      } else {
        const dur = p['duration'];
        const durStr = (dur != null && Number.isFinite(dur) && dur > 0) ? fmtRuntime(dur * 1e7) : '';
        movieInfo.textContent = durStr ? `${tr('movie')} · ${durStr}` : tr('movie');
      }
    }
  }

  // Track lists
  renderTrackLists(p['track-list'] || []);

  if (_playbackInfoOpen) renderPlaybackInfoSheet(p);
}

function setLiveProgressHidden(hidden) {
  document.querySelector('.playback-progress')?.classList.toggle('hidden', hidden);
  if (hidden) {
    _setText('health-cache-speed', '');
    const cacheFill = document.getElementById('progress-cache-fill');
    if (cacheFill) cacheFill.style.width = '0%';
  }
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

/* ── IPTV quality/variant switcher sheet ──────────────────────────────────── */
async function openIPTVQualitySheet() {
  if (!currentIPTVChannelId) return;
  _iptvQualityOpen = true;
  document.getElementById('iptv-quality-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
  const list = document.getElementById('iptv-quality-list');
  if (list) list.innerHTML = `<div class="iptv-empty">${esc(tr('iptvRefreshing'))}</div>`;
  try {
    const ch = await api('GET', `/api/iptv/channel/${encodeURIComponent(currentIPTVChannelId)}?server_id=${encodeURIComponent(_playbackServerId)}`);
    const variants = ch.variants || [];
    currentIPTVVariantCount = variants.length;
    if (list) {
      list.innerHTML = variants.map((v, i) => {
        const active = i === currentIPTVVariantIndex;
        return `
        <button class="track-pill${active ? ' active' : ''}" onclick="selectIPTVVariant(${i})" aria-pressed="${active}">${esc(v.label || `#${i + 1}`)}</button>
      `;
      }).join('') || `<div class="iptv-empty">${esc(tr('iptvNoChannels'))}</div>`;
    }
  } catch (e) {
    if (list) list.innerHTML = `<div class="iptv-empty">${esc(e.message)}</div>`;
  }
}
function closeIPTVQualitySheet() {
  _iptvQualityOpen = false;
  document.getElementById('iptv-quality-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
}
function onIPTVQualityBackdropClick(event) {
  if (event.target.id === 'iptv-quality-backdrop') closeIPTVQualitySheet();
}
async function selectIPTVVariant(index) {
  closeIPTVQualitySheet();
  if (!currentIPTVChannelId) return;
  try {
    await api('POST', '/api/player/play', { server_id: _playbackServerId, channel_id: currentIPTVChannelId, variant_index: index });
    currentIPTVVariantIndex = index;
    _fetchAndUpdateProps();
  } catch (e) {
    toast(e.message, true);
  }
}

/// Re-opens the current channel's stream fresh. IPTV has no DVR/seek UI, so
/// this is how the user (or the post-resume stall watchdog below) jumps back
/// to the live edge instead of continuing from wherever local buffering left
/// off.
async function iptvBackToLive() {
  if (!currentIPTVChannelId) return;
  try {
    await api('POST', '/api/player/play', { server_id: _playbackServerId, channel_id: currentIPTVChannelId, variant_index: currentIPTVVariantIndex });
    toast(tr('iptvBackToLiveToast'));
    _fetchAndUpdateProps();
  } catch (e) {
    toast(e.message, true);
  }
}

// Normal pause/resume for IPTV now behaves like any VOD source (resumes from
// the paused position, same as mpv's default), matching the movie-channel use
// case. The one thing IPTV needs that VOD doesn't: if resuming after a pause
// finds the stream stalled (mpv's cache is small and a long pause can outlast
// it, or the upstream connection dropped while idle), fall back to rejoining
// live automatically instead of leaving the viewer stuck buffering forever.
function togglePlayPause() {
  const wasPaused = !!_latestProps.pause;
  const isLive = isCurrentIPTVPlayback();
  cmd(['cycle', 'pause']);
  clearTimeout(_iptvResumeWatchdog);
  if (isLive && wasPaused) {
    _iptvResumeWatchdog = setTimeout(checkIPTVResumeHealth, 4000);
  }
}

async function checkIPTVResumeHealth() {
  if (!isCurrentIPTVPlayback()) return;
  try {
    const props = await api('GET', '/api/player/props');
    // If the user paused again during the grace window, that's their call to
    // make, not a stall — only auto-resync while actually (still) playing.
    if (!props.pause && isPlaybackStalled(props)) {
      await iptvBackToLive();
    }
  } catch (_) { /* props temporarily unavailable; skip this check */ }
}

/* ── IPTV programme guide sheet ───────────────────────────────────────────── */
function _iptvProgrammeTimeLabel(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return '';
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}
async function openIPTVProgrammeSheet() {
  if (!currentIPTVChannelId || !currentIPTVHasProgramme) return;
  _iptvProgrammeOpen = true;
  document.getElementById('iptv-programme-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
  const list = document.getElementById('iptv-programme-list');
  if (list) list.innerHTML = `<div class="iptv-empty">${esc(tr('iptvRefreshing'))}</div>`;
  try {
    const data = await api('GET', `/api/iptv/programme?channel_id=${encodeURIComponent(currentIPTVChannelId)}&count=4`);
    const rows = [];
    if (data.current) {
      rows.push(`<div class="iptv-programme-row current">
        <div class="iptv-programme-when">${esc(tr('iptvCurrentProgramme'))} · ${esc(_iptvProgrammeTimeLabel(data.current.start))}</div>
        <div class="iptv-programme-title">${esc(data.current.title || '')}</div>
      </div>`);
    }
    (data.upcoming || []).forEach(p => {
      rows.push(`<div class="iptv-programme-row">
        <div class="iptv-programme-when">${esc(_iptvProgrammeTimeLabel(p.start))}</div>
        <div class="iptv-programme-title">${esc(p.title || '')}</div>
      </div>`);
    });
    if (list) {
      list.innerHTML = rows.join('') || `<div class="iptv-empty">${esc(tr('iptvNoProgrammeInfo'))}</div>`;
    }
  } catch (e) {
    if (list) list.innerHTML = `<div class="iptv-empty">${esc(tr('iptvNoProgrammeInfo'))}</div>`;
  }
}
function closeIPTVProgrammeSheet() {
  _iptvProgrammeOpen = false;
  document.getElementById('iptv-programme-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
}
function onIPTVProgrammeBackdropClick(event) {
  if (event.target.id === 'iptv-programme-backdrop') closeIPTVProgrammeSheet();
}

function iptvChannelHasProgramme(channel) {
  return !!(channel && (channel.has_epg || (channel.current_programme && channel.current_programme.title)));
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
  // This drives the prominent decode-method badge, which should read as plainly
  // as "hardware/software decode" — the specific decoder API name (VideoToolbox,
  // D3D11VA, nvdec, ...) is a detail most users don't recognize; it still shows
  // in the secondary "decoder" row via _fmtHwdecName.
  if (!h || h === 'no') return tr('softwareDecode');
  return tr('hardwareDecode');
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
const ALL_SOURCE_TYPES = ['emby', 'jellyfin', 'plex', 'webdav', 'smb', 'local', 'nfs', 'iptv'];

function _normType(type) {
  const t = String(type || 'emby').toLowerCase();
  return ALL_SOURCE_TYPES.includes(t) ? t : 'emby';
}

// webdav/smb/local/nfs all browse a folder tree instead of a poster wall —
// tvOS only ever had webdav/smb (no local filesystem access in the
// sandbox); local/nfs are desktop-only additions that get the exact same
// treatment everywhere this is checked.
function isFileSourceType(type) {
  return ['webdav', 'smb', 'local', 'nfs'].includes(_normType(type));
}

function isIPTVSourceType(type) {
  return _normType(type) === 'iptv';
}

function sourceTypeLabel(type) {
  if (_normType(type) === 'dlna') return tr('dlnaReceiver');
  return {
    emby: 'Emby', jellyfin: 'Jellyfin', plex: 'Plex', webdav: 'WebDAV', smb: 'SMB',
    local: tr('sourceTypeLocalTitle'), nfs: tr('sourceTypeNFSTitle'), iptv: 'IPTV',
  }[_normType(type)];
}

function sourceTypeClass(type) { return 'st-' + _normType(type); }

function defaultSourceName(type) {
  const key = {
    emby: 'defaultEmbyName', jellyfin: 'defaultJellyfinName', plex: 'defaultPlexName',
    webdav: 'defaultWebDAVName', smb: 'defaultSMBName', local: 'defaultLocalName',
    nfs: 'defaultNFSName', iptv: 'defaultIPTVName',
  }[_normType(type)];
  return tr(key);
}

function _sourceBadge(type) {
  const t = _normType(type);
  return `<span class="source-badge source-${t}">${esc(sourceTypeLabel(t))}</span>`;
}

// Avatar chip shown in the server dropdown/manager list: a colored square with
// the source type's first letter (Emby/Jellyfin/Plex/WebDAV/SMB/Local/NFS/IPTV).
function _sourceAvatarHtml(type) {
  const t = _normType(type);
  return `<span class="smi-avatar ${sourceTypeClass(t)}">${esc(sourceTypeLabel(t)[0] || '?')}</span>`;
}

function _sourceMeta(s) {
  if (isFileSourceType(s.type)) {
    const parts = [sourceTypeLabel(s.type)];
    if (s.type === 'smb' && s.share) parts.push(s.share);
    if (s.root_path) parts.push(_truncate(s.root_path, 24));
    return parts.join(' · ');
  }
  if (isIPTVSourceType(s.type)) {
    try {
      return s.playlist_url ? new URL(s.playlist_url).host : '';
    } catch (_) {
      return _truncate(s.playlist_url || '', 30);
    }
  }
  const hosts = s.hosts || [];
  const host = hosts[s.active_host || 0] || '?';
  return `${host}:${s.port}`;
}

async function refreshServerSwitcher() {
  try {
    const servers = await api('GET', '/api/servers');
    const active  = servers.find(s => s.active);
    _activeServerId = active?.id || '';
    _activeSourceType = _normType(active && active.type);
    _hasAnyServer = servers.length > 0;
    updateLibraryEmptyState(!_hasAnyServer);
    document.getElementById('search-toggle')?.classList.toggle(
      'hidden',
      _activeTab !== 'library' || isFileSourceType(_activeSourceType) || _activeSourceType === 'iptv' || !_hasAnyServer
    );

    // Update header button
    const label = document.getElementById('active-server-label');
    const dot   = document.getElementById('server-dot');
    if (active) {
      label.textContent = active.name || tr('server');
      dot.classList.toggle('online', active.logged_in || isFileSourceType(active.type) || isIPTVSourceType(active.type));
    } else {
      label.textContent = tr('notConfigured');
      dot.classList.remove('online');
    }

    // Render menu
    const menu = document.getElementById('server-menu');
    let html = servers.map(s => {
      const isFile = isFileSourceType(s.type);
      const hosts = s.hosts || [];
      const activeHostIndex = s.active_host || 0;
      const hostButtons = (!isFile && hosts.length > 1)
        ? `<div class="server-menu-hosts">
            ${hosts.map((h, i) => `
              <button class="server-menu-host ${i === activeHostIndex ? 'active' : ''}"
                      onclick="switchServerHost(event, '${s.id}', ${i}, ${s.active})">
                <span class="smh-dot"></span>${esc(h)}
              </button>`).join('')}
          </div>`
        : '';
      return `<div class="server-menu-entry ${s.active ? 'active' : ''}">
        <button class="server-menu-item" type="button"
                onclick="switchServer('${s.id}', '${jsStr(s.name || tr('server'))}')">
          ${_sourceAvatarHtml(s.type)}
          <span class="smi-body">
            <span class="smi-name">${esc(s.name || tr('server'))}</span>
            <span class="smi-host">${esc(_sourceMeta(s))}</span>
          </span>
          <span class="smi-check" aria-hidden="true">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>
          </span>
        </button>
        ${hostButtons}
      </div>`;
    }).join('');
    html += `<button class="server-menu-manage" type="button" onclick="openServerManager();closeServerMenu()">
      <svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 15.5A3.5 3.5 0 1 0 12 8a3.5 3.5 0 0 0 0 7.5Z"/><path d="M19.4 15a1.8 1.8 0 0 0 .36 1.98l.04.04a2.15 2.15 0 1 1-3.04 3.04l-.04-.04a1.8 1.8 0 0 0-1.98-.36 1.8 1.8 0 0 0-1.08 1.65V21.4a2.15 2.15 0 1 1-4.3 0v-.09a1.8 1.8 0 0 0-1.08-1.65 1.8 1.8 0 0 0-1.98.36l-.04.04a2.15 2.15 0 1 1-3.04-3.04l.04-.04A1.8 1.8 0 0 0 4.6 15a1.8 1.8 0 0 0-1.65-1.08h-.09a2.15 2.15 0 1 1 0-4.3h.09A1.8 1.8 0 0 0 4.6 8.54a1.8 1.8 0 0 0-.36-1.98l-.04-.04a2.15 2.15 0 1 1 3.04-3.04l.04.04a1.8 1.8 0 0 0 1.98.36A1.8 1.8 0 0 0 10.34 2.2v-.09a2.15 2.15 0 1 1 4.3 0v.09a1.8 1.8 0 0 0 1.08 1.65 1.8 1.8 0 0 0 1.98-.36l.04-.04a2.15 2.15 0 1 1 3.04 3.04l-.04.04a1.8 1.8 0 0 0-.36 1.98 1.8 1.8 0 0 0 1.65 1.08h.09a2.15 2.15 0 1 1 0 4.3h-.09A1.8 1.8 0 0 0 19.4 15Z"/></svg>
      <span>${tr('manageServers')}</span>
    </button>`;
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
  const switchSeq = ++_serverSwitchSeq;
  beginLibraryLoad(serverId);
  closeServerMenu();
  closeLibraryPicker();
  // Optimistic label update so header reflects the change immediately
  if (serverName) _setText('active-server-label', serverName);
  _showLibrarySkeletons();
  try {
    const result = await api('POST', `/api/servers/${serverId}/activate`, {
      switch_session: _serverSwitchSession, switch_sequence: switchSeq,
    });
    if (switchSeq !== _serverSwitchSeq || result.active_server_id !== serverId) return;
    _activeServerId = serverId;
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
  const switchSeq = ++_serverSwitchSeq;
  beginLibraryLoad(serverId);
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
      const result = await api('POST', `/api/servers/${serverId}/activate`, {
        switch_session: _serverSwitchSession, switch_sequence: switchSeq,
      });
      if (result.active_server_id !== serverId) return;
    }
    if (switchSeq !== _serverSwitchSeq) return;
    _activeServerId = serverId;
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
  if (!_hasAnyServer || !_activeServerId) {
    resetLibraryState();
    updateLibraryEmptyState(true);
    return;
  }
  updateLibraryEmptyState(false);
  resetSearchUi();
  if (isFileSourceType(_activeSourceType)) {
    _enterFileMode();
    await loadDir('');
    return;
  }
  if (_activeSourceType === 'iptv') {
    await _enterIPTVMode();
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
    const data = await api('GET', '/api/library/libraries');
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
  browseMode = 'library';
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
      const data = await api('GET', `/api/library/items?${qs}`);
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
      const data = await api('GET', `/api/library/items?${qs}`);
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

async function viewAllResume() {
  resetSearchUi();
  browseMode = 'resume';
  browseStart = 0;
  browseHasMore = false;
  _setViewMode('resume');
  setBrowseTitle();
  await loadBrowse(false, { loadingMessage: tr('recent') });
}

function returnFromResume() {
  switchLibrary('', tr('all'));
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
  browseMode = 'library';
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
    const pageSize = browseMode === 'resume' ? RESUME_PAGE_SIZE : PAGE_SIZE;
    const qs = new URLSearchParams({ start: browseStart, limit: pageSize });
    if (browseMode === 'library' && browseParentId) qs.set('parent_id', browseParentId);
    const endpoint = browseMode === 'resume' ? '/api/library/resume' : '/api/library/items';
    const data = await api('GET', `${endpoint}?${qs}`);
    const items = data.Items || [];
    const totalCount = data.TotalRecordCount || 0;
    if (requestId !== browseLoadSeq) return;
    browseHasMore = browseStart + items.length < totalCount;
    browseStart  += items.length;

    const grid = document.getElementById('poster-grid');
    if (!append) grid.innerHTML = '';
    if (!items.length && !append) {
      grid.innerHTML = `<div class="search-state"><div class="search-state-title">${tr('noBrowseContentTitle')}</div><div class="search-state-subtitle">${tr('noBrowseContentSub')}</div></div>`;
    } else {
      const renderCard = browseMode === 'resume' ? resumeGridCardHtml : posterCardHtml;
      grid.insertAdjacentHTML('beforeend', items.map(renderCard).join(''));
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
    browseMode === 'resume' ? tr('recent') : (browseParentId ? currentLibraryName : tr('browse'));
  _setText('browse-back-label', tr('back'));
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
      <img src="${libraryImageUrl(itemId, maxHeight)}"
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
    const data  = await api('GET', '/api/library/resume');
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
  const imgSrc = libraryImageUrl(posterItemId, 400, 'Backdrop');
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

function resumeGridCardHtml(item) {
  const seriesTitle = item.SeriesName || '';
  const title = seriesTitle || item.Name || '';
  const episodeLabel = episodeLabelForItem(item);
  const subtitle = seriesTitle ? (episodeLabel || item.Name || '') : (item.ProductionYear || '');
  const posterItemId = item.SeriesId || item.Id;
  const pct = Math.round(item.UserData?.PlayedPercentage || 0);
  const progress = pct > 0 ? `<div class="resume-progress"><div class="resume-progress-fill" style="width:${Math.min(100, pct)}%"></div></div>` : '';
  return `
    <div class="poster-card" onclick="openVideoSheet('${item.Id}')">
      ${posterFrameHtml(posterItemId, title, 'poster-frame', 300, '', progress)}
      <div class="poster-card-info">
        <div class="poster-title">${esc(title)}</div>
        ${subtitle ? `<div class="poster-year">${esc(subtitle)}</div>` : ''}
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
    const item = await api('GET', `/api/library/items/${encodeURIComponent(itemId)}`);
    if (item.Type === 'Episode' && item.SeriesId) {
      // Open the parent series sheet and scroll to this episode.
      const series = await api('GET', `/api/library/items/${encodeURIComponent(item.SeriesId)}`);
      _locateEpisode = { id: item.Id, num: Number(item.IndexNumber) || null, seasonId: item.SeasonId || '' };
      renderSeriesSheet(series);
      await initSheetSeasons(series.Id);
      _locateEpisode = null;
    } else if (item.Type === 'Series') {
      renderSeriesSheet(item);
      await initSheetSeasons(item.Id);
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
  const variants = Array.isArray(item.PlaybackVariants) ? item.PlaybackVariants : [];
  _sheetMediaSourceId = variants[0]?.id || '';
  _sheetPlayableItem = { itemId: item.Id, seriesId, seasonId, title, seriesTitle, episodeLabel, posterItemId };
  const picker = variants.length > 1 ? `<div class="sheet-variants"><div class="sheet-variants-title">${tr('mediaVersion')}</div>${variants.map((v,i) => `<button class="sheet-variant${i===0?' active':''}" data-source-id="${esc(v.id)}" onclick="selectSheetVariant('${jsStr(v.id)}')"><b>${esc(v.height ? v.height+'p' : (v.name || tr('mediaVersion')))}</b><small>${esc([v.video_codec, v.container].filter(Boolean).join(' · ').toUpperCase())}</small></button>`).join('')}</div>` : '';
  document.getElementById('video-sheet-content').innerHTML = `
    ${sheetHeroHtml(item, subtitle)}
    ${picker}
    <div class="sheet-actions">
      <button class="sheet-play" onclick="playSelectedSheetItem()">${tr('play')}</button>
    </div>`;
}

function selectSheetVariant(id) {
  _sheetMediaSourceId = id;
  document.querySelectorAll('.sheet-variant').forEach(el => el.classList.toggle('active', el.dataset.sourceId === id));
}
function playSelectedSheetItem() {
  const i = _sheetPlayableItem; if (!i) return;
  playItem(i.itemId, i.seriesId, i.seasonId, i.title, i.seriesTitle, i.episodeLabel, i.posterItemId, _sheetMediaSourceId);
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
  _sheetEpisodePage = 0;
  currentSeriesId = item.Id;
  currentSeriesTitle = item.Name || '';
  const sortLabel = _sheetEpisodeSort === 'asc' ? tr('sortAsc') : tr('sortDesc');
  document.getElementById('video-sheet-content').innerHTML = `
    ${sheetHeroHtml(item, tr('series'))}
    <div class="season-chip-row hidden" id="season-chip-row"></div>
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

// Fetches the season list for a series and, if it has more than one season,
// shows a season picker defaulting to whichever season contains the episode
// being located (or the one currently playing). Older backends without a
// seasons endpoint (or a single-season show) fall back transparently to the
// flat, all-episodes list this sheet used before.
async function initSheetSeasons(seriesId) {
  sheetSeasons = [];
  sheetSeasonId = '';
  _sheetEpisodePage = 0;
  try {
    const data = await api('GET', `/api/library/seasons?series_id=${encodeURIComponent(seriesId)}`);
    const seasons = (data.Items || []).filter(s => s && s.Id);
    if (seasons.length > 1) {
      sheetSeasons = seasons;
      sheetSeasonId = _pickDefaultSeason(seasons, seriesId);
    }
  } catch (_) {
    // Seasons API unavailable — keep the flat list behavior.
  }
  const row = document.getElementById('season-chip-row');
  if (row) {
    row.innerHTML = sheetSeasons.map(seasonChipHtml).join('');
    row.classList.toggle('hidden', !sheetSeasons.length);
  }
  if (_locateEpisode && _locateEpisode.num) {
    _sheetEpisodePage = _pageForEpisodeNum(_locateEpisode.num);
  } else if (currentSeriesId === seriesId && currentItemId &&
             (!sheetSeasons.length || sheetSeasonId === currentSeasonId)) {
    _sheetEpisodePage = _estimateEpisodePage();
  }
  await loadSheetEpisodes(seriesId);
}

function _pickDefaultSeason(seasons, seriesId) {
  const locateSeasonId = _locateEpisode && _locateEpisode.seasonId;
  if (locateSeasonId && seasons.some(s => s.Id === locateSeasonId)) return locateSeasonId;
  if (currentSeriesId === seriesId && currentSeasonId && seasons.some(s => s.Id === currentSeasonId)) {
    return currentSeasonId;
  }
  const nonSpecial = seasons.find(s => (s.IndexNumber || 0) > 0);
  return (nonSpecial || seasons[0]).Id;
}

function seasonChipHtml(season) {
  const active = season.Id === sheetSeasonId;
  const label = season.Name || ((season.IndexNumber || 0) > 0 ? tr('seasonNum', { num: season.IndexNumber }) : tr('specials'));
  return `<button class="season-chip${active ? ' active' : ''}" data-season-id="${season.Id}" onclick="selectSheetSeason('${season.Id}')">${esc(label)}</button>`;
}

function selectSheetSeason(seasonId) {
  if (seasonId === sheetSeasonId || sheetEpisodesLoading) return;
  sheetSeasonId = seasonId;
  _sheetEpisodePage = 0;
  document.querySelectorAll('#season-chip-row .season-chip').forEach(el => {
    el.classList.toggle('active', el.dataset.seasonId === seasonId);
  });
  loadSheetEpisodes(sheetSeriesId);
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
    const data = await api('GET', `/api/library/items?${qs}`);
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
      season_id: sheetSeasonId || '',
      start: pageStart,
      limit: EPISODE_PAGE_SIZE,
      sort: _sheetEpisodeSort,
    });
    const data = await api('GET', `/api/library/episodes?${qs}`);
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
    const data = await api('GET', `/api/library/items?search=${encodeURIComponent(value)}&limit=60`);
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
      dlna_receiver_enabled: settings.dlna_receiver_enabled !== false,
    };
    _supportedFileProtocols = Array.isArray(settings.supported_file_protocols)
      ? settings.supported_file_protocols
      : null;
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
  _setText('cache-minutes-range-hint', tr('cacheMinutesRangeHint'));
  _setText('dlna-receiver-title', tr('dlnaReceiverTitle'));
  _setText('dlna-receiver-hint', tr('dlnaReceiverHint'));
  const dlnaToggle = document.getElementById('dlna-receiver-toggle');
  if (dlnaToggle) dlnaToggle.checked = _settings.dlna_receiver_enabled !== false;
  _setText('dlna-receiver-status', tr(_settings.dlna_receiver_enabled !== false ? 'dlnaReceiverOn' : 'dlnaReceiverOff'));

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

async function toggleDLNAReceiver() {
  const enabled = document.getElementById('dlna-receiver-toggle')?.checked !== false;
  try {
    const saved = await api('PUT', '/api/settings', { dlna_receiver_enabled: enabled });
    _settings.dlna_receiver_enabled = saved.dlna_receiver_enabled !== false;
    _setDLNAReceiverStatus(tr('settingsSaved'));
  } catch (e) {
    _setDLNAReceiverStatus(e.message || tr('settingsSaveFailed'), true);
  }
  renderSettingsUi();
}

function _setDLNAReceiverStatus(message, isError = false) {
  const el = document.getElementById('dlna-receiver-settings-status');
  if (!el) return;
  el.textContent = message || '';
  el.className = 'settings-status' + (message ? (isError ? ' err' : ' ok') : '');
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
  const raw = Number(input?.value);
  const minutes = Math.max(1, Math.min(120, Math.round(raw) || 30));
  // Clamp the visible value immediately when it's out of range, instead of
  // only clamping the saved value — otherwise the box can show e.g. 9999
  // until the field loses focus, which looks like there's no real cap.
  if (input && (Number.isNaN(raw) || raw > 120 || raw < 1)) {
    input.value = String(minutes);
  }
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
    _settings = { ..._settings, mpv_cache_secs: Number(saved.mpv_cache_secs) || _settings.mpv_cache_secs };
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
    const type = _normType(s.type);
    const fileSource = isFileSourceType(type);
    const iptvSource = isIPTVSourceType(type);
    const hosts = s.hosts || [];
    const activeH = s.active_host || 0;
    const host = hosts[activeH] || hosts[0] || '';
    const hostLine = host ? `${host}:${s.port}${type === 'smb' && s.share ? '/' + s.share : ''}` : '';
    const extra = hosts.length > 1 ? ` · +${hosts.length - 1}` : '';
    const needsLogin = !fileSource && !iptvSource && !s.logged_in;
    return `<div class="server-card ${s.active ? 'active-server' : ''}" onclick="openServerForm('${s.id}')">
      <span class="server-card-avatar ${sourceTypeClass(type)}">${esc(sourceTypeLabel(type)[0])}</span>
      <div class="server-card-body">
        <div class="server-card-name">${esc(s.name || tr('server'))}<span class="source-type-badge ${sourceTypeClass(type)}">${esc(sourceTypeLabel(type))}</span></div>
        ${hostLine ? `<div class="server-card-meta">${esc(hostLine + extra)}</div>` : ''}
        ${fileSource && s.root_path ? `<div class="server-card-meta">${esc(s.root_path)}</div>` : ''}
        ${iptvSource ? `<div class="server-card-meta" id="iptv-summary-${s.id}">${esc(_sourceMeta(s))}</div>` : ''}
        ${needsLogin ? `<div class="server-card-warn">${tr('notLoggedIn')}</div>` : ''}
      </div>
      <button class="server-card-delete" type="button" aria-label="${tr('delete')}" onclick="event.stopPropagation();confirmDeleteServer('${s.id}', '${jsStr(s.name || tr('server'))}')">×</button>
    </div>`;
  }).join('');
  servers.filter(s => _normType(s.type) === 'iptv').forEach(refreshIPTVServerCardStatus);
}

// Channel/EPG-matched counts shared by the settings server card and the
// library browser status line. EPG is dropped entirely when zero — most
// sources have no EPG configured, and "EPG 0" carries no information.
function _iptvCountParts(summary) {
  const parts = [tr('iptvChannelsCount', { count: summary.channel_count || 0 })];
  if (summary.epg_matched_count) parts.push(tr('iptvEpgMatched', { count: summary.epg_matched_count }));
  return parts;
}

async function refreshIPTVServerCardStatus(s) {
  try {
    const summary = await api('GET', `/api/iptv/summary?server_id=${encodeURIComponent(s.id)}`);
    const el = document.getElementById(`iptv-summary-${s.id}`);
    if (!el) return;
    const parts = _iptvCountParts(summary);
    // "ok" is the expected steady state and is already implied by having
    // fresh counts; only surface the badge when there's something to flag.
    if (summary.refresh_status !== 'ok') {
      parts.push(tr(summary.refresh_status === 'error' ? 'iptvSourceStatusError' : 'iptvSourceStatusPending'));
    }
    el.textContent = parts.join(' · ');
    el.classList.toggle('iptv-status-error', summary.refresh_status === 'error');
  } catch (_) {}
}

/* ── Source type picker ───────────────────────────────────────────────────── */
function hideServerManagerSheet() {
  document.getElementById('server-manager-backdrop')?.classList.add('hidden');
}

function openSourceTypePicker() {
  hideServerManagerSheet();
  // Hide any file-source card this backend build can't actually serve,
  // rather than letting the user pick a type that always fails to connect.
  document.querySelectorAll('#source-type-backdrop .source-type-option').forEach(btn => {
    const type = (btn.getAttribute('onclick') || '').match(/chooseSourceType\('(\w+)'\)/)?.[1];
    const supported = !isFileSourceType(type) || !_supportedFileProtocols || _supportedFileProtocols.includes(type);
    btn.classList.toggle('hidden', !supported);
  });
  document.getElementById('source-type-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
}

function closeSourceTypePicker() {
  document.getElementById('source-type-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
  openServerManager();
}

function closeSourceTypePickerOnly() {
  document.getElementById('source-type-backdrop')?.classList.add('hidden');
}

function onSourceTypeBackdropClick(event) {
  if (event.target.id === 'source-type-backdrop') closeSourceTypePicker();
}

function chooseSourceType(type) {
  openServerForm('', type || 'emby');
}

function updateSourceTypeReadout(type) {
  const value = type || 'emby';
  const input = document.getElementById('form-type');
  const label = document.getElementById('form-type-label');
  if (input) input.value = value;
  if (label) label.textContent = sourceTypeLabel(value);
  const name = document.getElementById('form-name');
  if (name) name.placeholder = defaultSourceName(value);
}

/* ── Add / edit server form ───────────────────────────────────────────────── */
let _serverFormGeneration = 0;

function openServerForm(serverId, sourceType = 'emby') {
  const formGeneration = ++_serverFormGeneration;
  hideServerManagerSheet();
  closeSourceTypePickerOnly();
  document.getElementById('server-form-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
  const form = document.getElementById('server-form');
  const title = document.getElementById('server-form-title');
  if (title) title.textContent = serverId ? tr('editServer') : tr('addServer');

  document.getElementById('form-server-id').value = serverId || '';
  document.getElementById('form-status').textContent = '';
  const hostIds = ['form-host0', 'form-host1', 'form-host2'];
  updateSourceTypeReadout(sourceType || 'emby');

  const clearForm = () => {
    document.getElementById('form-name').value = '';
    document.querySelectorAll('input[name="proto"]').forEach(r => { r.checked = r.value === 'http'; });
    hostIds.forEach(id => { document.getElementById(id).value = ''; });
    document.getElementById('form-port').value = sourceDefaultPort(sourceType || 'emby', 'http');
    document.getElementById('form-share').value = '';
    document.getElementById('form-root-path').value = '';
    document.getElementById('form-domain').value = '';
    document.getElementById('form-token').value = '';
    document.getElementById('form-username').value = '';
    document.getElementById('form-password').value = '';
    document.getElementById('form-iptv-playlist').value = '';
    document.getElementById('form-iptv-epg').value = '';
    const advanced = document.getElementById('form-advanced-options');
    if (advanced) advanced.open = false;
    resetPasswordReveal();
  };

  clearForm();
  if (serverId) {
    // Populate from existing server
    api('GET', '/api/servers').then(servers => {
      if (formGeneration !== _serverFormGeneration
          || document.getElementById('server-form-backdrop')?.classList.contains('hidden')) return;
      const s = servers.find(x => x.id === serverId);
      if (!s) return;
      updateSourceTypeReadout(s.type || 'emby');
      document.getElementById('form-name').value     = s.name || '';
      document.querySelectorAll('input[name="proto"]').forEach(r => { r.checked = r.value === (s.protocol || 'http'); });
      const hosts = s.hosts || [];
      hostIds.forEach((id, i) => { document.getElementById(id).value = hosts[i] || ''; });
      const advanced = document.getElementById('form-advanced-options');
      if (advanced) advanced.open = hosts.length > 1;
      document.getElementById('form-port').value     = s.port || 8096;
      document.getElementById('form-share').value    = s.share || '';
      document.getElementById('form-root-path').value = s.root_path || '';
      document.getElementById('form-domain').value   = s.domain || '';
      document.getElementById('form-username').value = s.username || '';
      document.getElementById('form-password').value = '';
      document.getElementById('form-iptv-playlist').value = s.playlist_url || '';
      document.getElementById('form-iptv-epg').value = s.epg_url || '';
      resetPasswordReveal();
      onSourceTypeChange(false);
    });
  } else {
    onSourceTypeChange(false);
  }

  const sheet = document.querySelector('#server-form-backdrop .server-form-sheet');
  if (sheet) sheet.scrollTop = 0;
  form.scrollIntoView({ behavior: 'smooth', block: 'start' });
}

function togglePasswordReveal() {
  const input = document.getElementById('form-password');
  const btn = document.getElementById('form-password-reveal');
  if (!input || !btn) return;
  const reveal = input.type === 'password';
  input.type = reveal ? 'text' : 'password';
  btn.classList.toggle('revealed', reveal);
  btn.setAttribute('aria-pressed', reveal ? 'true' : 'false');
  btn.setAttribute('aria-label', tr(reveal ? 'hidePassword' : 'showPassword'));
}

function resetPasswordReveal() {
  const input = document.getElementById('form-password');
  const btn = document.getElementById('form-password-reveal');
  if (input) input.type = 'password';
  if (btn) {
    btn.classList.remove('revealed');
    btn.setAttribute('aria-pressed', 'false');
    btn.setAttribute('aria-label', tr('showPassword'));
  }
}

function closeServerForm() {
  _serverFormGeneration++;
  document.getElementById('server-form-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
  openServerManager();
}

function closeServerFormOnly() {
  _serverFormGeneration++;
  document.getElementById('server-form-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
}

async function _onSaveSuccess(id) {
  // Finishing add/edit (or, for file sources, picking the folder) is a
  // deliberate "use this now" signal — always activate it, regardless of
  // whether another source was already active.
  if (id) await api('POST', `/api/servers/${id}/activate`);
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

function normalizeSourceInput(type, protocol, hosts, port, rootPath = '', share = '') {
  if (!hosts.length || !/^[a-z][a-z0-9+.-]*:\/\//i.test(hosts[0])) return { protocol, hosts, port, rootPath, share };
  try {
    const url = new URL(hosts[0]);
    protocol = type === 'smb' ? protocol : url.protocol.replace(':', '');
    port = url.port ? Number(url.port) : (protocol === 'https' ? 443 : sourceDefaultPort(type, protocol));
    hosts = [url.hostname, ...hosts.slice(1)].filter(Boolean);
    const parts = url.pathname.split('/').filter(Boolean).map(decodeURIComponent);
    if (type === 'webdav' && parts.length && !rootPath) rootPath = parts.join('/');
    if (type === 'smb' && parts.length) {
      if (!share) share = parts.shift() || '';
      if (!rootPath) rootPath = parts.join('/');
    }
  } catch (_) {}
  return { protocol, hosts, port, rootPath, share };
}

async function saveAndLogin() {
  const statusEl  = document.getElementById('form-status');
  const serverId  = document.getElementById('form-server-id').value;
  const type      = document.getElementById('form-type').value || 'emby';
  const name      = document.getElementById('form-name').value.trim() || defaultSourceName(type);
  const saveButton = document.getElementById('form-save-btn');
  const setStatus = (msg, cls = '') => { statusEl.textContent = msg; statusEl.className = 'form-status' + (cls ? ' ' + cls : ''); };

  // ── IPTV: no hosts/protocol/credentials, just a playlist + optional EPG URL.
  if (isIPTVSourceType(type)) {
    const playlistUrl = document.getElementById('form-iptv-playlist').value.trim();
    const epgUrl = document.getElementById('form-iptv-epg').value.trim();
    if (!playlistUrl) { setStatus(tr('needPlaylistUrl'), 'err'); return; }
    setStatus(tr('iptvRefreshing'));
    if (saveButton) saveButton.disabled = true;
    try {
      let id = serverId;
      if (serverId) {
        await api('PUT', `/api/servers/${serverId}`, { name, type, playlist_url: playlistUrl, epg_url: epgUrl });
        // PUT only patches config; unlike add, it doesn't refresh the parsed
        // channel/EPG cache, so a changed URL needs an explicit refresh here.
        await api('POST', `/api/iptv/refresh?server_id=${encodeURIComponent(serverId)}`);
      } else {
        const srv = await api('POST', '/api/servers', { name, type, playlist_url: playlistUrl, epg_url: epgUrl });
        id = srv.id;
      }
      setStatus(tr('saved'), 'ok');
      await _onSaveSuccess(id);
    } catch (e) { setStatus(e.message, 'err'); }
    finally { if (saveButton) saveButton.disabled = false; }
    return;
  }

  // ── File sources (webdav / smb / local / nfs) ──────────────────────────────
  // The share/sub-folder path is never typed here — saving just proves the
  // source is reachable, then hands off to the folder picker so the actual
  // path is chosen by browsing (see openFolderPicker below).
  if (isFileSourceType(type)) {
    const networked = type === 'webdav' || type === 'smb';
    let protocol  = document.querySelector('input[name="proto"]:checked')?.value || 'http';
    let hosts     = networked
      ? ['form-host0', 'form-host1', 'form-host2'].map(id => document.getElementById(id).value.trim()).filter(Boolean)
      : [];
    let port      = networked ? (parseInt(document.getElementById('form-port').value, 10) || sourceDefaultPort(type, protocol)) : 0;
    const username  = document.getElementById('form-username').value.trim();
    const password  = document.getElementById('form-password').value;
    // Hidden state carriers: never directly typed, only ever set by the
    // folder picker (or, for share, by SMB's share-enumeration browse step).
    let share       = document.getElementById('form-share').value.trim();
    let rootPath    = document.getElementById('form-root-path').value.trim();
    const domain      = type === 'smb' ? document.getElementById('form-domain').value.trim() : '';

    ({ protocol, hosts, port, rootPath, share } = normalizeSourceInput(type, protocol, hosts, port, rootPath, share));

    if (networked && !hosts.length) { setStatus(tr('needOneHost'), 'err'); return; }

    setStatus(tr('connecting'));
    if (saveButton) saveButton.disabled = true;
    const payload = { name, type, protocol, hosts, port, username, share, domain };
    try {
      let id = serverId;
      if (serverId) {
        if (password) payload.password = password;   // blank → keep existing
        await api('PUT', `/api/servers/${serverId}`, payload);
        await api('POST', `/api/servers/${serverId}/connect`, {});
      } else {
        const srv = await api('POST', '/api/servers', { ...payload, password });
        id = srv.id;
      }
      setStatus(tr('connected'), 'ok');
      closeServerFormOnly();
      const baseRootPath = type === 'smb' && share ? [share, rootPath].filter(Boolean).join('/') : rootPath;
      openFolderPicker(id, type, baseRootPath);
    } catch (e) {
      setStatus(e.message, 'err');
    } finally {
      if (saveButton) saveButton.disabled = false;
    }
    return;
  }

  // ── Media server (emby / jellyfin / plex) ─────────────────────────────────
  let protocol = document.querySelector('input[name="proto"]:checked')?.value || 'http';
  let hosts    = ['form-host0', 'form-host1', 'form-host2']
    .map(id => document.getElementById(id).value.trim()).filter(Boolean);
  let port     = parseInt(document.getElementById('form-port').value, 10) || sourceDefaultPort(type, protocol);
  const username = document.getElementById('form-username').value.trim();
  const password = document.getElementById('form-password').value;
  const token    = document.getElementById('form-token').value.trim();
  ({ protocol, hosts, port } = normalizeSourceInput(type, protocol, hosts, port));
  const hasCreds = (username && password) || (type === 'plex' && token);

  if (!hosts.length){ setStatus(tr('needOneHost'), 'err'); return; }
  setStatus(tr('saving'));
  if (saveButton) saveButton.disabled = true;

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
  } finally {
    if (saveButton) saveButton.disabled = false;
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
/* ── Server form: type-driven field visibility ────────────────────────────── */
function sourceDefaultPort(type, protocol = 'http') {
  if (type === 'smb') return 445;
  if (type === 'webdav') return protocol === 'https' ? 443 : 80;
  if (type === 'plex') return protocol === 'https' ? 443 : 32400;
  if (type === 'local' || type === 'nfs') return 0; // no network fields shown at all
  // Public Emby/Jellyfin deployments normally terminate TLS on the standard
  // HTTPS port. Self-hosted 8920 remains available by editing the port field.
  return protocol === 'https' ? 443 : 8096;
}

function onSourceTypeChange(updatePort = true) {
  const type = document.getElementById('form-type')?.value || 'emby';
  const protocol = document.querySelector('input[name="proto"]:checked')?.value || 'http';
  const fileSource = isFileSourceType(type);
  const iptvSource = isIPTVSourceType(type);
  // local/nfs have no host/credentials at all — the folder picker is the
  // entire "connection" step, so every network-ish field is hidden.
  const networkless = type === 'local' || type === 'nfs';

  document.getElementById('form-protocol-row')?.classList.toggle('hidden', type === 'smb' || networkless || iptvSource);
  document.getElementById('form-host0-row')?.classList.toggle('hidden', networkless || iptvSource);
  document.getElementById('form-port-row')?.classList.toggle('hidden', networkless || iptvSource);
  document.getElementById('form-domain-row')?.classList.toggle('hidden', type !== 'smb');
  const advanced = document.getElementById('form-advanced-options');
  if (advanced) {
    advanced.classList.toggle('hidden', networkless || iptvSource);
    if (networkless || iptvSource) advanced.open = false;
  }
  const localHintRow = document.getElementById('form-local-hint-row');
  localHintRow?.classList.toggle('hidden', !networkless);
  if (networkless) _setText('form-local-hint', type === 'nfs' ? tr('nfsHint') : tr('localHint'));
  document.getElementById('form-row-token')?.classList.toggle('hidden', type !== 'plex');
  // IPTV has no per-channel authentication, and local/nfs have no concept of
  // one either — the account/password fields would just sit there doing
  // nothing, so hide the whole block.
  document.getElementById('form-credentials-group')?.classList.toggle('hidden', networkless || iptvSource);
  document.getElementById('form-iptv-playlist-row')?.classList.toggle('hidden', !iptvSource);
  document.getElementById('form-iptv-epg-row')?.classList.toggle('hidden', !iptvSource);

  const host = document.getElementById('form-host0');
  if (host) {
    host.placeholder = type === 'smb' ? tr('smbHostPlaceholder')
      : (type === 'webdav' ? 'dav.example.com' : tr('mediaHostPlaceholder'));
  }
  const username = document.getElementById('form-username');
  const password = document.getElementById('form-password');
  if (username) username.placeholder = type === 'plex' ? tr('plexUsernamePlaceholder') : '';
  if (password) password.placeholder = type === 'plex' ? tr('plexPasswordPlaceholder') : '';
  _setText('form-token-hint', tr('plexTokenHint'));
  if (updatePort && !iptvSource && !networkless) {
    document.getElementById('form-port').value = sourceDefaultPort(type, protocol);
  }
  const saveButton = document.getElementById('form-save-btn');
  if (saveButton) saveButton.textContent = tr((fileSource || iptvSource) ? 'saveAndConnect' : 'saveAndLogin');
}

function onSourceProtocolChange() {
  const type = document.getElementById('form-type')?.value || 'emby';
  if (type !== 'smb') onSourceTypeChange(true);
}

/* ── Folder picker (WebDAV / SMB / local / NFS root-path selection) ──────── */
let _fpServerId = '';
let _fpType = '';
let _fpPath = '';
let _fpBaseRootPath = ''; // root_path (± share, for smb) already on the server, if any

function openFolderPicker(serverId, type, baseRootPath = '') {
  _fpServerId = serverId;
  _fpType = type || '';
  _fpPath = '';
  _fpBaseRootPath = baseRootPath || '';
  const hint = document.getElementById('folder-picker-hint');
  if (hint) hint.textContent = tr('folderPickerHint');
  document.getElementById('folder-picker-backdrop').classList.remove('hidden');
  document.body.classList.add('sheet-open');
  _fpNavigate('');
}

// Cancel or backdrop: close without committing anything.
function closeFolderPickerCancel() {
  document.getElementById('folder-picker-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');
  openServerManager();
}

function onFolderPickerBackdropClick(event) {
  if (event.target.id === 'folder-picker-backdrop') closeFolderPickerCancel();
}

// Left action button: use the root while in a subfolder, or cancel at the root.
async function closeFolderPickerSkip() {
  if (_fpPath) {
    // For SMB, "root" means the selected share's root, not the host's share list.
    _fpPath = _fpType === 'smb' ? (_fpPath.split('/').filter(Boolean)[0] || '') : '';
    await confirmFolderPicker();
  } else {
    closeFolderPickerCancel();
  }
}

async function confirmFolderPicker() {
  const path = _fpPath;
  document.getElementById('folder-picker-backdrop').classList.add('hidden');
  document.body.classList.remove('sheet-open');

  // Effective root_path = base (already on the server, for a re-browse) + the
  // newly-picked sub-path.
  const effectivePath = [_fpBaseRootPath, path].filter(Boolean).join('/');
  if (!_fpServerId) return;

  const payload = {};
  if (_fpType === 'smb') {
    const parts = effectivePath.split('/').filter(Boolean);
    payload.share = parts.shift() || '';
    payload.root_path = parts.join('/');
  } else {
    payload.root_path = effectivePath;
  }
  try {
    await api('PUT', `/api/servers/${_fpServerId}`, payload);
    await _onSaveSuccess(_fpServerId);
  } catch (e) {
    toast(e.message, true);
    await refreshServerSwitcher();
  }
}

async function _fpNavigate(path) {
  _fpPath = path;
  _fpRenderBreadcrumb(path);
  const listEl = document.getElementById('folder-picker-list');
  const confirmBtn = document.getElementById('folder-picker-confirm-btn');
  const skipBtn = document.getElementById('folder-picker-skip-btn');
  if (confirmBtn) {
    confirmBtn.textContent = _fpType === 'smb' && !path ? tr('folderPickerChooseShare') : ((path || _fpBaseRootPath) ? tr('folderPickerUseThis') : tr('folderPickerUseRoot'));
    confirmBtn.disabled = _fpType === 'smb' && !path;
  }
  if (skipBtn) skipBtn.textContent = path ? tr('folderPickerUseRoot') : tr('cancel');
  if (listEl) listEl.innerHTML = `<div class="fp-loading">${tr('folderPickerLoadingFolders')}</div>`;
  try {
    const qs = new URLSearchParams({ server_id: _fpServerId });
    if (path) qs.set('path', path);
    const data = await api('GET', `/api/files/list?${qs}`);
    const dirs = (data.entries || []).filter(item => item.is_dir);
    if (!listEl) return;
    if (!dirs.length) {
      listEl.innerHTML = `<div class="fp-empty">${tr('folderPickerNoFolders')}</div>`;
      return;
    }
    listEl.innerHTML = dirs.map(item => `
      <div class="fp-folder-row" onclick="_fpNavigate('${jsStr(item.path)}')">
        <svg class="fp-folder-icon" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
          <path d="M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2z"/>
        </svg>
        <span>${esc(item.name)}</span>
        <svg class="fp-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>
      </div>`).join('');
  } catch (e) {
    if (listEl) listEl.innerHTML = `<div class="fp-error">${esc(e.message)}</div>`;
  }
}

function _fpRenderBreadcrumb(path) {
  const el = document.getElementById('folder-picker-breadcrumb');
  if (!el) return;
  const parts = path ? path.split('/').filter(Boolean) : [];
  let html = `<button class="fp-crumb" onclick="_fpNavigate('')">${tr('rootDirectory')}</button>`;
  parts.forEach((p, i) => {
    const pathTo = parts.slice(0, i + 1).join('/');
    html += `<span class="fp-crumb-sep">›</span><button class="fp-crumb" onclick="_fpNavigate('${jsStr(pathTo)}')">${esc(p)}</button>`;
  });
  el.innerHTML = html;
}

/* ── Settings danger zone: reset all configuration ────────────────────────── */
function openResetConfigurationConfirm() {
  document.getElementById('reset-config-confirm-overlay')?.classList.remove('hidden');
}

function closeResetConfigurationConfirm() {
  document.getElementById('reset-config-confirm-overlay')?.classList.add('hidden');
}

async function resetAllConfiguration() {
  const button = document.getElementById('reset-config-confirm-btn');
  if (button) button.disabled = true;
  try {
    await api('POST', '/api/settings/reset', {});
    localStorage.clear();
    sessionStorage.clear();
    window.location.reload();
  } catch (error) {
    if (button) button.disabled = false;
    closeResetConfigurationConfirm();
    toast(error.message || tr('settingsSaveFailed'), true);
  }
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

/* ── IPTV channel browser ─────────────────────────────────────────────────── */
function _enterIPTVMode() {
  switchTab('library');
  document.getElementById('search-toggle')?.classList.add('hidden');
  _setViewMode('iptv');
  _iptvCategory = 'all';
  _iptvSearch = '';
  return Promise.all([loadIPTVSourceStatus(), loadIPTVCategories(), loadIPTVChannels()]);
}

function _exitIPTVMode() {
  document.getElementById('search-toggle')?.classList.toggle('hidden', _activeTab !== 'library');
}

async function loadIPTVSourceStatus() {
  const el = document.getElementById('iptv-source-status');
  if (!el) return;
  el.innerHTML = `<span>${esc(tr('iptvRefreshing'))}</span>`;
  try {
    const s = await api('GET', '/api/iptv/summary');
    renderIPTVSourceStatus(s);
  } catch (e) {
    el.innerHTML = `<span class="iptv-status-error">${esc(e.message)}</span>`;
  }
}

function renderIPTVSourceStatus(s) {
  const el = document.getElementById('iptv-source-status');
  if (!el) return;
  const parts = _iptvCountParts(s);
  if (s.last_refreshed) {
    const time = new Date(s.last_refreshed);
    if (!isNaN(time)) parts.push(tr('iptvLastUpdated', { time: time.toLocaleString() }));
  }
  // "ok" is the expected steady state and is already implied by having
  // fresh counts; only show a second line when there's something to flag.
  const badge = s.refresh_status === 'ok' ? '' : `
    <span class="${s.refresh_status === 'error' ? 'iptv-status-error' : ''}">${esc(tr(
      s.refresh_status === 'error' ? 'iptvSourceStatusError' : 'iptvSourceStatusPending'
    ))}</span>`;
  el.innerHTML = `<span class="iptv-source-status-line">${parts.map(esc).join(' · ')}</span>${badge}`;
}

async function refreshIPTVSource() {
  const button = document.getElementById('btn-iptv-refresh');
  if (button) { button.disabled = true; button.classList.add('spinning'); }
  try {
    const s = await api('POST', '/api/iptv/refresh');
    renderIPTVSourceStatus(s);
    await Promise.all([loadIPTVCategories(), loadIPTVChannels()]);
    toast(tr('iptvRefreshed'));
  } catch (e) {
    // The backend keeps the last known-good channels on a failed refresh but
    // does flip refresh_status to "error" — reload the status line so it
    // reflects that instead of silently keeping a stale "ok" badge.
    await loadIPTVSourceStatus();
    toast(e.message || tr('iptvRefreshFailed'), true);
  } finally {
    if (button) { button.disabled = false; button.classList.remove('spinning'); }
  }
}

async function loadIPTVCategories() {
  try {
    const categories = await api('GET', '/api/iptv/categories');
    renderIPTVCategoryPills(categories);
  } catch (_) {
    renderIPTVCategoryPills(['all', 'favorites', 'recent']);
  }
}

function _iptvCategoryLabel(cat) {
  if (cat === 'all') return tr('categoryAll');
  if (cat === 'favorites') return tr('categoryFavorites');
  if (cat === 'recent') return tr('categoryRecent');
  return cat;
}

function renderIPTVCategoryPills(categories) {
  const row = document.getElementById('iptv-category-row');
  if (!row) return;
  row.innerHTML = categories.map(cat => `
    <button class="iptv-category-pill${cat === _iptvCategory ? ' active' : ''}"
            onclick="selectIPTVCategory('${jsStr(cat)}')">${esc(_iptvCategoryLabel(cat))}</button>
  `).join('');
}

function selectIPTVCategory(cat) {
  if (cat === _iptvCategory) return;
  _iptvCategory = cat;
  document.querySelectorAll('#iptv-category-row .iptv-category-pill').forEach(btn => {
    btn.classList.toggle('active', btn.textContent === _iptvCategoryLabel(cat));
  });
  loadIPTVChannels();
}

async function loadIPTVChannels() {
  const list = document.getElementById('iptv-channel-list');
  if (list) list.innerHTML = `<div class="iptv-empty">${esc(tr('iptvRefreshing'))}</div>`;
  try {
    const params = new URLSearchParams();
    if (_iptvCategory && _iptvCategory !== 'all') params.set('category', _iptvCategory);
    if (_iptvSearch) params.set('search', _iptvSearch);
    const channels = await api('GET', `/api/iptv/channels?${params.toString()}`);
    _iptvChannels = channels;
    renderIPTVChannelList(channels);
  } catch (e) {
    if (list) list.innerHTML = `<div class="iptv-empty">${esc(e.message || tr('iptvLoadChannelsFailed'))}</div>`;
  }
}

function _iptvChannelLogoHtml(channel) {
  const letter = esc((channel.name || '?').trim()[0] || '?');
  const fallback = `<div class="iptv-channel-logo-fallback">${letter}</div>`;
  if (!channel.logo_url) return `<div class="iptv-channel-logo-wrap">${fallback}</div>`;
  return `<div class="iptv-channel-logo-wrap">
    ${fallback}
    <img class="iptv-channel-logo" src="${esc(channel.logo_url)}" alt="" loading="lazy" onerror="this.remove()">
  </div>`;
}

function _iptvChannelRowHtml(ch) {
  const metaParts = [ch.group_title].filter(Boolean);
  // Graceful no-EPG fallback: current_programme is simply absent, no
  // separate "no guide" row layout — just one less piece of meta text.
  if (ch.current_programme && ch.current_programme.title) metaParts.push(ch.current_programme.title);
  return `
    <div class="iptv-channel-row" role="button" tabindex="0"
         onclick="playIPTVChannel('${jsStr(ch.id)}', ${ch.variant_count || 1})"
         onkeydown="if(event.key==='Enter')playIPTVChannel('${jsStr(ch.id)}', ${ch.variant_count || 1})">
      ${_iptvChannelLogoHtml(ch)}
      <div class="iptv-channel-body">
        <div class="iptv-channel-name-row">
          <span class="iptv-channel-name">${esc(ch.name)}</span>
          ${ch.quality ? `<span class="iptv-channel-quality">${esc(ch.quality)}</span>` : ''}
        </div>
        <div class="iptv-channel-meta">${esc(metaParts.join(' · '))}</div>
      </div>
      <button class="iptv-channel-favorite${ch.is_favorite ? ' active' : ''}"
              aria-label="${ch.is_favorite ? esc(tr('iptvRemoveFromFavorites')) : esc(tr('iptvAddToFavorites'))}"
              onclick="toggleIPTVChannelFavorite(event, '${jsStr(ch.id)}')">${ch.is_favorite ? '★' : '☆'}</button>
    </div>`;
}

// Large playlists (e.g. iptv-org's ~12k-channel index.m3u) blow the DOM up to
// tens of thousands of nodes if rendered in one innerHTML pass. Render in
// batches and grow the list as the user scrolls near the bottom instead.
const IPTV_RENDER_BATCH = 60;
let _iptvRenderedCount = 0;
let _iptvRenderObserver = null;

function renderIPTVChannelList(channels) {
  const list = document.getElementById('iptv-channel-list');
  if (!list) return;
  if (_iptvRenderObserver) { _iptvRenderObserver.disconnect(); _iptvRenderObserver = null; }
  if (!channels.length) {
    list.innerHTML = `<div class="iptv-empty">${esc(tr('iptvNoChannels'))}</div>`;
    return;
  }
  list.innerHTML = '';
  _iptvChannels = channels;
  _iptvRenderedCount = 0;
  _appendIPTVChannelBatch(list);
}

function _appendIPTVChannelBatch(list) {
  const next = _iptvChannels.slice(_iptvRenderedCount, _iptvRenderedCount + IPTV_RENDER_BATCH);
  if (!next.length) return;
  list.insertAdjacentHTML('beforeend', next.map(_iptvChannelRowHtml).join(''));
  _iptvRenderedCount += next.length;

  document.getElementById('iptv-channel-list-sentinel')?.remove();
  if (_iptvRenderedCount >= _iptvChannels.length) return;

  const sentinel = document.createElement('div');
  sentinel.id = 'iptv-channel-list-sentinel';
  list.appendChild(sentinel);
  _iptvRenderObserver = new IntersectionObserver((entries) => {
    if (entries.some(e => e.isIntersecting)) {
      _iptvRenderObserver.disconnect();
      _appendIPTVChannelBatch(list);
    }
  });
  _iptvRenderObserver.observe(sentinel);
}

async function toggleIPTVChannelFavorite(event, channelId) {
  event.stopPropagation();
  try {
    await api('POST', '/api/iptv/favorite', { channel_id: channelId });
    await loadIPTVChannels();
  } catch (e) {
    toast(e.message, true);
  }
}

async function playIPTVChannel(channelId, variantCount = 1) {
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
  _playbackServerId = _activeServerId;
  currentIPTVChannelId = channelId;
  currentIPTVVariantIndex = 0;
  currentIPTVVariantCount = variantCount;
  const channel = _iptvChannels.find(c => c.id === channelId);
  currentIPTVHasProgramme = iptvChannelHasProgramme(channel);
  setNowPlaying(channel ? channel.name : '', { isLive: true });
  switchTab('remote');
  document.getElementById('play-loading-tag')?.classList.remove('hidden');
  document.getElementById('player-info-placeholder')?.classList.remove('hidden');
  document.getElementById('player-info')?.classList.add('hidden');
  try {
    await api('POST', '/api/player/play', { server_id: _playbackServerId, channel_id: channelId, variant_index: 0 });
    _fetchAndUpdateProps();
  } catch (e) {
    toast(e.message, true);
    setNowPlaying('');
  } finally {
    document.getElementById('play-loading-tag')?.classList.add('hidden');
  }
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
        <button class="btn-secondary" onclick="loadDir(_filesPath)">${tr('retry')}</button>
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
  _playbackServerId = _activeServerId;
  setNowPlaying(name, {});
  switchTab('remote');
  document.getElementById('play-loading-tag')?.classList.remove('hidden');
  document.getElementById('player-info-placeholder')?.classList.remove('hidden');
  document.getElementById('player-info')?.classList.add('hidden');
  try {
    await api('POST', '/api/player/play', { server_id: _playbackServerId, path, title: name });
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

/* ── Player engine sheet (mpv is the only desktop engine — informational) ── */
function openEngineSheet() {
  document.getElementById('engine-backdrop')?.classList.remove('hidden');
  document.body.classList.add('sheet-open');
}
function closeEngineSheet() {
  document.getElementById('engine-backdrop')?.classList.add('hidden');
  document.body.classList.remove('sheet-open');
}
function onEngineBackdropClick(event) {
  if (event.target.id === 'engine-backdrop') closeEngineSheet();
}

/* ── Playback debug report ("Playback issue?" button) ────────────────────── */
let _playbackDebugOpen = false;
let _playbackDebugReportText = '';

function openPlaybackDebugSheet() {
  _playbackDebugOpen = true;
  _playbackDebugReportText = '';
  const backdrop = document.getElementById('playback-debug-backdrop');
  const preview = document.getElementById('playback-debug-preview');
  const status = document.getElementById('playback-debug-status');
  const button = document.getElementById('playback-debug-copy');
  if (preview) preview.textContent = tr('playbackDebugLoading');
  if (status) { status.textContent = ''; status.className = 'playback-debug-status'; }
  if (button) button.disabled = true;
  if (backdrop) backdrop.classList.remove('hidden');
  document.body.classList.add('sheet-open');
  _loadPlaybackDebugReport();
}

function closePlaybackDebugSheet() {
  _playbackDebugOpen = false;
  document.getElementById('playback-debug-backdrop')?.classList.add('hidden');
  const engineSheetOpen = !document.getElementById('engine-backdrop')?.classList.contains('hidden');
  if (!engineSheetOpen) document.body.classList.remove('sheet-open');
}

function onPlaybackDebugBackdropClick(event) {
  if (event.target.id === 'playback-debug-backdrop') closePlaybackDebugSheet();
}

async function _loadPlaybackDebugReport() {
  const preview = document.getElementById('playback-debug-preview');
  const button = document.getElementById('playback-debug-copy');
  try {
    const data = await api('GET', '/api/player/debug-report');
    if (!_playbackDebugOpen) return;
    _playbackDebugReportText = _formatPlaybackDebugReport(data);
    if (preview) preview.textContent = _playbackDebugReportText;
    if (button) button.disabled = false;
  } catch (e) {
    if (!_playbackDebugOpen) return;
    _playbackDebugReportText = '';
    if (preview) preview.textContent = e.message || tr('playbackDebugLoadFailed');
    if (button) button.disabled = true;
  }
}

function _formatPlaybackDebugReport(data = {}) {
  const report = {
    ...data,
    remote_client: {
      user_agent: navigator.userAgent || '',
      language: navigator.language || '',
      online: navigator.onLine !== false,
      page_protocol: location.protocol,
    },
  };
  const safe = _englishPlaybackDebugData(_redactPlaybackDebugData(report));
  return [
    'TinyPlay Playback Debug Report',
    'Describe the exact symptom and reproduction steps above this report.',
    '============================================================',
    JSON.stringify(safe, null, 2),
  ].join('\n');
}

// Logs and titles can be in Chinese (app language, media names) but the report
// is read by an English-speaking developer, so anything non-ASCII is dropped
// rather than shipped untranslated.
function _englishPlaybackDebugData(value) {
  if (Array.isArray(value)) return value.map(_englishPlaybackDebugData);
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([key, child]) => [
      key, _englishPlaybackDebugData(child),
    ]));
  }
  if (typeof value !== 'string') return value;
  return value.replace(/[　-〿㐀-䶿一-鿿豈-﫿＀-￯]+/g, '[non-English text omitted]');
}

function _redactPlaybackDebugData(value, key = '') {
  const sensitiveKey = /^(authorization|proxy-authorization|password|passwd|token|access_token|api_key|apikey|x-emby-token|http_headers?|headers)$/i;
  if (sensitiveKey.test(key)) return '[redacted]';
  if (Array.isArray(value)) return value.map(item => _redactPlaybackDebugData(item));
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([childKey, childValue]) => [
      childKey,
      _redactPlaybackDebugData(childValue, childKey),
    ]));
  }
  if (typeof value !== 'string') return value;
  return value
    .replace(/([a-z][a-z0-9+.-]*:\/\/)[^/@\s]+@/gi, '$1[redacted]@')
    .replace(/([?&](?:api_key|apikey|token|access_token|password|auth|authorization)=)[^&\s]*/gi, '$1redacted')
    .replace(/(Bearer\s+)[A-Za-z0-9._~+\/-]+=*/gi, '$1[redacted]');
}

async function copyPlaybackDebugReport() {
  if (!_playbackDebugReportText) return;
  const status = document.getElementById('playback-debug-status');
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(_playbackDebugReportText);
    } else {
      _copyTextWithTemporaryTextarea(_playbackDebugReportText);
    }
    if (status) { status.textContent = tr('playbackDebugCopied'); status.className = 'playback-debug-status'; }
  } catch (_) {
    try {
      _copyTextWithTemporaryTextarea(_playbackDebugReportText);
      if (status) { status.textContent = tr('playbackDebugCopied'); status.className = 'playback-debug-status'; }
    } catch (_) {
      _selectPlaybackDebugPreview();
      if (status) { status.textContent = tr('playbackDebugCopyManual'); status.className = 'playback-debug-status manual'; }
    }
  }
}

function _selectPlaybackDebugPreview() {
  const preview = document.getElementById('playback-debug-preview');
  if (!preview || !window.getSelection || !document.createRange) return;
  const selection = window.getSelection();
  const range = document.createRange();
  range.selectNodeContents(preview);
  selection.removeAllRanges();
  selection.addRange(range);
}

function _copyTextWithTemporaryTextarea(text) {
  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.style.position = 'fixed';
  textarea.style.opacity = '0';
  document.body.appendChild(textarea);
  textarea.select();
  textarea.setSelectionRange(0, textarea.value.length);
  const copied = document.execCommand('copy');
  textarea.remove();
  if (!copied) throw new Error('copy failed');
}

function toast(msg, isError = false) {
  // Backend error text can be arbitrarily long (e.g. a raw network error);
  // cap it so one bad message can't blow the toast past the viewport.
  const text = String(msg).length > 200 ? `${String(msg).slice(0, 200)}…` : msg;
  const el = Object.assign(document.createElement('div'), {
    textContent: text,
    style: `
      position:fixed;bottom:80px;left:50%;transform:translateX(-50%);
      max-width:min(85vw,420px);
      background:${isError ? 'var(--danger)' : 'var(--accent)'};
      color:#fff;padding:10px 18px;border-radius:20px;font-size:13px;
      z-index:999;white-space:normal;word-break:break-word;text-align:center;
      pointer-events:none;
      animation:fadeUp .25s ease`,
  });
  document.body.appendChild(el);
  setTimeout(() => el.remove(), isError ? 4000 : 2800);
}
