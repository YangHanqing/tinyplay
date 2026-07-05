(function () {
  var ZH = 'zh-CN';
  var EN = 'en';

  var messages = {};
  messages[ZH] = {
    page_title: 'TinyPlay — 榨干小主机，也把它变成客厅播放器',
    meta_description: 'TinyPlay：把家里的 NUC、小主机或 Mac mini 变成由手机遥控的客厅播放器。',
    nav_why: '为什么',
    nav_features: '功能',
    nav_choose: '选购指南',
    nav_appletv: '查看 Apple TV',
    nav_download: '下载',
    hero_eyebrow: '手机控制，电脑播放',
    hero_title: '手机是遥控器，<br><em>电脑负责播放。</em>',
    hero_lead: '接上电视的电脑负责真正的播放，你窝在沙发上用手机选片、拖进度条、调倍速——不用再摸键盘鼠标，也不用满沙发缝找遥控器。那台电脑正好可以是家里已经在跑 Docker 的 NUC、迷你主机，或一台 Mac mini。',
    btn_download_app: '下载 macOS / Windows <span>↗</span>',
    btn_view_appletv: '查看 Apple TV',
    platform_win: 'Windows x86-64',
    platform_mac: 'macOS Apple Silicon',
    platform_phone: '手机无需安装 App',
    signal_lan: '局域网已连接',
    signal_room: '客厅播放器',
    why_title: '那台 24 小时开机的小主机，<br>其实离电视播放器只差一步。',
    why1_title: '性能已经在那里了',
    why1_body: '很多人家里那台 NUC 或迷你主机——哪怕只是台 N100——本来就在做下载机、跑 Docker、管理 NAS。它功耗不高、长期在线，还有一个经常闲着的 HDMI 接口。没必要再买一台只能做一件事的设备。',
    why2_title: '但"能播放"不等于"好控制"',
    why2_body: '电脑接上电视很简单，真正麻烦的是窝在沙发里选片、拖进度、切字幕：键盘鼠标不属于客厅，远程桌面又太重。',
    why3_title: '于是有了 TinyPlay',
    why3_body: '选片、拖进度条、切换倍速——这些你会反复动手的操作，全部挪到手机上完成。桌面端连接 Emby、Jellyfin、Plex 或 SMB/WebDAV 共享并驱动内置的万能播放器，手机扫码即可浏览媒体库。播放留在电脑，控制回到你手里。',
    features_title: '电脑负责兼容性，<br>手机负责顺手。',
    features_lead: '内置的万能播放器负责真正的播放，TinyPlay 负责把媒体库、遥控器和桌面播放器连在一起。没有云端中转，操作都发生在你的家庭局域网里。',
    feature1_title: '手机浏览媒体库',
    feature1_body: '连接 Emby、Jellyfin、Plex，或直接浏览 SMB/WebDAV。',
    feature2_title: '完整播放控制',
    feature2_body: '暂停、跳转、倍速、系统音量、画面比例与缩放。',
    feature3_title: '音轨与字幕',
    feature3_body: '切换轨道、调整字幕延迟，不再起身碰电脑。',
    feature4_title: '扫码即用',
    feature4_body: '手机不装 App、不注册账号，打开浏览器就是遥控器。',
    flow_title: '三步，把 HDMI 用起来。',
    flow1_title: '安装',
    flow1_body: '在 Windows 小主机或 Apple Silicon Mac 上运行 TinyPlay。',
    flow2_title: '连接',
    flow2_body: '添加媒体服务器或网络共享，让电脑通过 HDMI 连接电视。',
    flow3_title: '开播',
    flow3_body: '手机扫描二维码，选片、播放，坐回沙发。',
    choose_title: '电视柜里，哪种设备更适合你？',
    choose_lead: '没有绝对的赢家，只有不同的取舍。TinyPlay 最适合那台你已经拥有、也愿意继续榨干性能的电脑。',
    t_dimension: '维度',
    t_col_nuc: 'NUC / Windows 小主机',
    t_col_mac: 'M 芯片 Mac mini',
    t_col_atv: 'Apple TV 4K',
    t_col_bluray: '专业蓝光机',
    t_row_position: '核心定位',
    t_position_nuc: '可折腾的全能 HTPC',
    t_position_mac: '电脑 + 播放器',
    t_position_atv: '流媒体盒子',
    t_position_bluray: '光盘与影音专机',
    t_row_docker: '下载 / Docker',
    t_docker_nuc: '最自由',
    t_docker_mac: '很强',
    t_docker_atv: '不适合',
    t_docker_bluray: '不支持',
    t_row_nas: '本地媒体 / NAS',
    t_nas_nuc: '格式和软件选择最多',
    t_nas_mac: '很强',
    t_nas_atv: '需第三方播放器',
    t_nas_bluray: '不是主要场景',
    t_row_streaming: '流媒体',
    t_streaming_nuc: '浏览器与桌面 App',
    t_streaming_mac: '浏览器与桌面 App',
    t_streaming_atv: '最省心',
    t_streaming_bluray: '能力有限',
    t_row_hd_audio: '高清音频直通',
    t_hd_audio_nuc: '上限最高',
    t_hd_audio_mac: '不以源码直通见长',
    t_hd_audio_atv: '不以本地高清直通见长',
    t_hd_audio_bluray: '最稳妥',
    t_row_hdr: 'HDR / 杜比视界',
    t_hdr_nuc: '看显卡与播放器，上限高但要自己调',
    t_hdr_mac: '支持 HDR10，杜比视界受限',
    t_hdr_atv: '开箱即用、最省心',
    t_hdr_bluray: '原生杜比视界，最稳',
    t_row_maintenance: '维护成本',
    t_maintenance_nuc: '高',
    t_maintenance_mac: '中',
    t_maintenance_atv: '低',
    t_maintenance_bluray: '低',
    t_row_best_for: '最适合',
    t_best_nuc: '已有小主机，愿意折腾并榨干性能',
    t_best_mac: '希望电视柜里也是一台完整电脑',
    t_best_atv: '流媒体优先、全家省心',
    t_best_bluray: 'UHD 蓝光收藏与功放玩家',
    v1_tag: '最省心',
    v1_body: '流媒体与家庭使用体验最好，本地播放交给成熟的第三方 App。',
    v2_tag: '最能榨干性能',
    v2_title: 'NUC / Windows 小主机',
    v2_body: '下载、Docker、NAS 管理和播放一机多用；自由度最高，也需要更多调校。',
    v3_tag: '最全能',
    v3_title: 'M 芯片 Mac mini',
    v3_body: '播放、浏览、工作和家庭服务兼顾，适合想保留完整电脑体验的人。',
    v4_tag: '最纯粹',
    v4_title: '专业蓝光机',
    v4_body: '面向实体光盘、AV 功放和稳定的影音体验，但用途最单一。',
    guide_note: '实际 HDR、Dolby Vision 与音频能力会随芯片、操作系统、驱动、播放器、片源封装和影音设备而变化，购买前请以具体设备规格为准。',
    download_title: '让那台小主机，<br>今晚就接管电视。',
    download_platforms: 'Windows x86-64 · macOS Apple Silicon',
    footer_tagline: 'Turn the little box into the big screen.',
    footer_license: 'MIT License',
    appletv_modal_title: 'Apple TV 版本',
    appletv_modal_body: '正在开发中，敬请期待。',
    appletv_modal_close: '好的',
  };
  messages[EN] = {
    page_title: 'TinyPlay — squeeze out your mini PC, turn it into a living-room player',
    meta_description: 'TinyPlay turns the NUC, mini PC, or Mac mini already running in your home into a phone-controlled living-room player.',
    nav_why: 'Why',
    nav_features: 'Features',
    nav_choose: 'Buying guide',
    nav_appletv: 'View Apple TV',
    nav_download: 'Download',
    hero_eyebrow: 'Phone control, computer playback',
    hero_title: 'Your phone is the remote.<br><em>The computer does the playing.</em>',
    hero_lead: 'The computer already hooked up to your TV handles the actual playback. From the couch, pick a title, scrub the progress bar, and change the speed — all from your phone, no remote-hunting or keyboard-and-mouse required. That computer can just as well be the NUC or mini PC already running Docker in the background, or a Mac mini.',
    btn_download_app: 'Download macOS / Windows <span>↗</span>',
    btn_view_appletv: 'View Apple TV',
    platform_win: 'Windows x86-64',
    platform_mac: 'macOS Apple Silicon',
    platform_phone: 'No app needed on your phone',
    signal_lan: 'Connected on LAN',
    signal_room: 'Living-room player',
    why_title: 'That mini PC that’s already on 24/7<br>is one step away from a TV player.',
    why1_title: 'The performance is already there',
    why1_body: 'Plenty of people already have a NUC or mini PC at home — even a humble N100 — quietly downloading, running Docker, managing a NAS. Low power draw, always on, and usually an idle HDMI port. No need to buy another single-purpose box.',
    why2_title: '"Can play" isn’t "easy to control"',
    why2_body: 'Plugging a computer into the TV is the easy part. The annoying part is picking a title, scrubbing, switching subtitles from the couch: a keyboard and mouse don’t belong in the living room, and remote desktop is overkill.',
    why3_title: 'So we built TinyPlay',
    why3_body: 'Picking a title, scrubbing, switching playback speed — the parts you touch again and again — all move to your phone. The desktop app connects to Emby, Jellyfin, Plex, or an SMB/WebDAV share and drives a built-in do-it-all player; scan a QR code to browse the library. Playback stays on the computer, control comes back to your hand.',
    features_title: 'The computer handles compatibility,<br>the phone handles convenience.',
    features_lead: 'A built-in do-it-all player handles the actual playback; TinyPlay connects your media library, remote, and desktop player together. No cloud relay — everything happens on your home LAN.',
    feature1_title: 'Browse your library from your phone',
    feature1_body: 'Connect Emby, Jellyfin, Plex, or browse SMB/WebDAV directly.',
    feature2_title: 'Full playback control',
    feature2_body: 'Pause, seek, speed, system volume, aspect ratio, and zoom.',
    feature3_title: 'Audio tracks and subtitles',
    feature3_body: 'Switch tracks and adjust subtitle delay without getting up.',
    feature4_title: 'Scan and go',
    feature4_body: 'No app to install, no account to create — open a browser and it’s a remote.',
    flow_title: 'Three steps to put that HDMI port to work.',
    flow1_title: 'Install',
    flow1_body: 'Run TinyPlay on your Windows mini PC or Apple Silicon Mac.',
    flow2_title: 'Connect',
    flow2_body: 'Add a media server or network share, and plug the computer into your TV over HDMI.',
    flow3_title: 'Play',
    flow3_body: 'Scan the QR code on your phone, pick something, and sit back down.',
    choose_title: 'Which box actually belongs in your TV stand?',
    choose_lead: 'There’s no single winner, just different trade-offs. TinyPlay fits best on a computer you already own and don’t mind squeezing further.',
    t_dimension: 'Dimension',
    t_col_nuc: 'NUC / Windows mini PC',
    t_col_mac: 'M-series Mac mini',
    t_col_atv: 'Apple TV 4K',
    t_col_bluray: 'Dedicated Blu-ray player',
    t_row_position: 'Core role',
    t_position_nuc: 'Tinkerable all-round HTPC',
    t_position_mac: 'Computer + player',
    t_position_atv: 'Streaming box',
    t_position_bluray: 'Disc & AV specialist',
    t_row_docker: 'Downloads / Docker',
    t_docker_nuc: 'Most freedom',
    t_docker_mac: 'Very capable',
    t_docker_atv: 'Not suited',
    t_docker_bluray: 'Not supported',
    t_row_nas: 'Local media / NAS',
    t_nas_nuc: 'Widest format & software choice',
    t_nas_mac: 'Very capable',
    t_nas_atv: 'Needs a third-party player',
    t_nas_bluray: 'Not a primary use case',
    t_row_streaming: 'Streaming',
    t_streaming_nuc: 'Browser & desktop apps',
    t_streaming_mac: 'Browser & desktop apps',
    t_streaming_atv: 'Easiest by far',
    t_streaming_bluray: 'Limited',
    t_row_hd_audio: 'HD audio passthrough',
    t_hd_audio_nuc: 'Highest ceiling',
    t_hd_audio_mac: 'Not known for bitstreaming',
    t_hd_audio_atv: 'Not known for local HD passthrough',
    t_hd_audio_bluray: 'Most reliable',
    t_row_hdr: 'HDR / Dolby Vision',
    t_hdr_nuc: 'Depends on GPU & player — high ceiling, needs tuning',
    t_hdr_mac: 'HDR10 works, Dolby Vision is limited',
    t_hdr_atv: 'Out of the box, easiest',
    t_hdr_bluray: 'Native Dolby Vision, most reliable',
    t_row_maintenance: 'Upkeep',
    t_maintenance_nuc: 'High',
    t_maintenance_mac: 'Medium',
    t_maintenance_atv: 'Low',
    t_maintenance_bluray: 'Low',
    t_row_best_for: 'Best for',
    t_best_nuc: 'You already have a mini PC and enjoy tinkering with it',
    t_best_mac: 'You want the TV stand to also hold a full computer',
    t_best_atv: 'Streaming-first, low-maintenance for the whole family',
    t_best_bluray: 'UHD disc collectors and AV receiver enthusiasts',
    v1_tag: 'Easiest',
    v1_body: 'Best streaming and family experience; leave local playback to mature third-party apps.',
    v2_tag: 'Squeezes the most out of it',
    v2_title: 'NUC / Windows mini PC',
    v2_body: 'Downloads, Docker, NAS management, and playback on one box; the most freedom, and the most tuning.',
    v3_tag: 'Most all-round',
    v3_title: 'M-series Mac mini',
    v3_body: 'Playback, browsing, work, and home services together — for people who want to keep a full computer.',
    v4_tag: 'Most purpose-built',
    v4_title: 'Dedicated Blu-ray player',
    v4_body: 'Built for physical discs, AV receivers, and a rock-solid experience, but the narrowest use case.',
    guide_note: 'Actual HDR, Dolby Vision, and audio capabilities vary by chip, OS, driver, player, source container, and AV equipment. Check your exact hardware’s specs before buying.',
    download_title: 'Let that mini PC<br>take over the TV tonight.',
    download_platforms: 'Windows x86-64 · macOS Apple Silicon',
    footer_tagline: 'Turn the little box into the big screen.',
    footer_license: 'MIT License',
    appletv_modal_title: 'Apple TV version',
    appletv_modal_body: 'In development — stay tuned.',
    appletv_modal_close: 'Got it',
  };

  var STORAGE_KEY = 'tinyplay_site_lang';

  function detectLang() {
    var stored = null;
    try { stored = localStorage.getItem(STORAGE_KEY); } catch (e) {}
    if (stored === ZH || stored === EN) return stored;
    var nav = (navigator.language || navigator.userLanguage || '').toLowerCase();
    return nav.indexOf('zh') === 0 ? ZH : EN;
  }

  function applyLang(lang) {
    document.documentElement.lang = lang;
    var dict = messages[lang] || messages[EN];
    document.querySelectorAll('[data-i18n]').forEach(function (el) {
      var key = el.getAttribute('data-i18n');
      if (dict[key] == null) return;
      if (el.hasAttribute('data-i18n-attr')) {
        el.setAttribute(el.getAttribute('data-i18n-attr'), dict[key]);
      } else {
        el.textContent = dict[key];
      }
    });
    document.querySelectorAll('[data-i18n-html]').forEach(function (el) {
      var key = el.getAttribute('data-i18n-html');
      if (dict[key] != null) el.innerHTML = dict[key];
    });
    document.querySelectorAll('#lang-toggle .lang-opt').forEach(function (el) {
      el.classList.toggle('active', el.getAttribute('data-lang') === lang);
    });
  }

  function toggleSiteLang() {
    var current = document.documentElement.lang === ZH ? ZH : EN;
    var next = current === ZH ? EN : ZH;
    try { localStorage.setItem(STORAGE_KEY, next); } catch (e) {}
    applyLang(next);
  }
  window.toggleSiteLang = toggleSiteLang;

  function openAppleTvModal(event) {
    if (event) event.preventDefault();
    document.getElementById('appletv-modal-backdrop').classList.remove('hidden');
  }
  window.openAppleTvModal = openAppleTvModal;

  function closeAppleTvModal() {
    document.getElementById('appletv-modal-backdrop').classList.add('hidden');
  }
  window.closeAppleTvModal = closeAppleTvModal;

  function onAppleTvBackdropClick(event) {
    if (event.target.id === 'appletv-modal-backdrop') closeAppleTvModal();
  }
  window.onAppleTvBackdropClick = onAppleTvBackdropClick;

  document.addEventListener('DOMContentLoaded', function () {
    applyLang(detectLang());
  });
})();
