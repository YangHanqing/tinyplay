(function () {
  var ZH = 'zh-CN';
  var EN = 'en';

  var messages = {};
  messages[ZH] = {
    page_title: 'TinyPlay — 榨干小主机，也把它变成客厅播放器',
    meta_description: 'TinyPlay：把接在电视上的 Windows 电脑或 Mac 变成由手机遥控的客厅播放器。',
    og_title: 'TinyPlay — 手机是遥控器，电脑负责播放。',
    og_description: '把接在电视上的电脑变成由手机遥控的客厅播放器。',
    nav_why: '为什么',
    nav_features: '功能',
    nav_choose: '选购指南',
    nav_guide: '使用指南',
    nav_appletv: 'Apple TV 即将推出',
    nav_download: '下载',
    hero_eyebrow: '手机控制，电脑播放',
    hero_title: '手机是遥控器，<br><em>电脑负责播放。</em>',
    hero_lead: '让接在电视上的 Windows 电脑或 Mac 处理播放；你只管用手机选片、拖进度、切字幕。无需安装手机 App，所有操作都留在家庭局域网。',
    btn_download_app: '下载 macOS / Windows <span>↗</span>',
    btn_view_appletv: 'Apple TV 版 · 即将推出',
    platform_win: 'Windows x86-64',
    platform_mac: 'macOS Apple Silicon / Intel',
    platform_phone: '手机无需安装 App',
    signal_lan: '局域网已连接',
    signal_room: '客厅播放器',
    why_title: '已有的电脑，<br>也能成为好用的电视播放器。',
    why1_title: '不必先买新硬件',
    why1_body: '常年开着的 x86 小主机（如 NUC）、Mac，甚至闲置的 Windows 笔记本，都可能胜任。只要它能稳定输出画面，并满足你片源所需的解码能力，就能把 HDMI 接到电视。',
    why2_title: '但"能播放"不等于"好控制"',
    why2_body: '电脑接上电视很简单，真正麻烦的是窝在沙发里选片、拖进度、切字幕：键盘鼠标不属于客厅，远程桌面又太重。',
    why3_title: '于是有了 TinyPlay',
    why3_body: '选片、拖进度条、切换字幕与倍速，全部挪到手机上完成。电脑连接媒体库、文件夹或直播源，再把播放交给随应用附带的 mpv；你不需要了解 mpv，只需让电脑处理格式兼容性。',
    features_eyebrow: 'FOUR ENTRANCES, ONE REMOTE',
    features_title: '四种内容入口，<br>一套手机遥控器。',
    features_lead: '无论内容在哪里，播放都留在接电视的电脑上；浏览、选择和控制都在你的手机里完成。',
    feature1_title: '媒体库',
    feature1_body: '连接 Emby、Jellyfin 或 Plex，浏览海报、剧集与继续观看。',
    feature2_title: '文件夹',
    feature2_body: '从本机、已挂载目录、SMB 或 WebDAV 中浏览和播放。',
    feature3_title: 'IPTV',
    feature3_body: '导入已有的 M3U/M3U8 播放列表，配合 XMLTV 查看节目。',
    feature4_title: 'DLNA 投屏',
    feature4_body: '让局域网内支持 DLNA 的应用把内容投到这台电脑上。',
    product_note: '暂停、跳转、倍速、音轨、字幕和画面控制始终使用同一套遥控器。播放由随应用附带的 mpv 完成。',
    hero_image_alt: '手机控制连接电视的电脑播放内容',
    remote_image_alt: 'TinyPlay 手机遥控器界面',
    library_image_alt: 'TinyPlay 手机媒体库界面',
    flow_title: '三步，把 HDMI 用起来。',
    flow1_title: '安装',
    flow1_body: '在 Windows 小主机或 Mac 上运行 TinyPlay。',
    flow2_title: '连接',
    flow2_body: '添加媒体服务器或网络共享，让电脑通过 HDMI 连接电视。',
    flow3_title: '开播',
    flow3_body: '手机扫描二维码，选片、播放，坐回沙发。',
    choose_title: '电视柜里，哪种设备更适合你？',
    choose_lead: '没有绝对的赢家，只有不同的取舍。先看你已有的设备和最常做的事，再决定是否需要添置新设备。',
    t_dimension: '维度',
    t_col_nuc: 'Windows 小主机 / 笔记本',
    t_col_mac: 'M 芯片 Mac mini',
    t_col_atv: 'Apple TV 4K',
    t_col_bluray: '本地影音播放器（如芝杜）',
    t_row_position: '核心定位',
    t_position_nuc: '可扩展的电脑播放器',
    t_position_mac: '电脑 + 播放器',
    t_position_atv: '流媒体盒子',
    t_position_bluray: '本地影视专机',
    t_row_docker: '下载 / Docker',
    t_docker_nuc: '可用',
    t_docker_mac: '可用',
    t_docker_atv: '不适合',
    t_docker_bluray: '不支持',
    t_row_nas: '本地媒体 / NAS',
    t_nas_nuc: '软件与格式选择多',
    t_nas_mac: '软件选择多',
    t_nas_atv: '通常依赖第三方 App',
    t_nas_bluray: '本地播放是主场',
    t_row_streaming: '流媒体',
    t_streaming_nuc: '浏览器与桌面 App',
    t_streaming_mac: '浏览器与桌面 App',
    t_streaming_atv: '开箱即用',
    t_streaming_bluray: '能力因应用而异',
    t_row_hd_audio: '高清音频直通',
    t_hd_audio_nuc: '看硬件、系统和设置',
    t_hd_audio_mac: '不以源码直通见长',
    t_hd_audio_atv: '不以本地高清直通见长',
    t_hd_audio_bluray: '看型号与音频格式',
    t_row_hdr: 'HDR / 杜比视界',
    t_hdr_nuc: '看显卡、系统与播放器',
    t_hdr_mac: '支持 HDR，具体能力看片源',
    t_hdr_atv: '流媒体体验省心',
    t_hdr_bluray: '本地影片体验看型号',
    t_row_maintenance: '维护成本',
    t_maintenance_nuc: '中到高',
    t_maintenance_mac: '中',
    t_maintenance_atv: '低',
    t_maintenance_bluray: '低到中',
    t_row_best_for: '最适合',
    t_best_nuc: '已有电脑，希望自由配置',
    t_best_mac: '希望电视柜里也是一台完整电脑',
    t_best_atv: '流媒体优先、全家省心',
    t_best_bluray: '本地影片优先，偏好专机',
    v1_tag: '流媒体优先',
    v1_body: '适合主要观看流媒体、希望全家上手简单的人。',
    v2_tag: '已有电脑',
    v2_title: 'Windows 小主机 / 笔记本',
    v2_body: '如果已有可接电视的电脑，TinyPlay 是把它变成客厅播放器的一种方式。',
    v3_tag: '兼顾日常',
    v3_title: 'M 芯片 Mac mini',
    v3_body: '播放、浏览、工作和家庭服务兼顾，适合想保留完整电脑体验的人。',
    v4_tag: '本地播放优先',
    v4_title: '本地影音播放器（如芝杜）',
    v4_body: '适合把本地影视播放作为主要用途、偏好独立影音设备的人。',
    guide_note: '实际 HDR、Dolby Vision 与音频能力会随芯片、操作系统、驱动、播放器、片源封装和影音设备而变化，购买前请以具体设备规格为准。',
    download_title: '让那台小主机，<br>今晚就接管电视。',
    download_platforms: 'Windows x86-64 · macOS Apple Silicon / Intel',
    footer_tagline: 'Turn the little box into the big screen.',
    footer_license: 'GPL-3.0 License',
    appletv_modal_title: 'Apple TV 版本',
    appletv_modal_body: '正在开发中，敬请期待。',
    appletv_modal_close: '好的',

    guide_page_title: '使用指南 — TinyPlay',
    guide_meta_description: 'TinyPlay 使用指南：已有媒体服务器、直接播放文件，或搭建一个媒体库，从适合你的方式开始。',
    guide_back_home: '← 返回首页',
    guide_eyebrow: 'GETTING STARTED',
    guide_title: '从这里开始，找到适合你的接入方式',
    guide_lead: '无论你已经在用 NAS，还是只想播放电脑里的一个文件夹，都能很快让 TinyPlay 跑起来。先找到和你当前情况最接近的一项。',

    guide_nav1_tag: '情况一',
    guide_nav1_title: '已有媒体服务器',
    guide_nav1_desc: '已经在用 Emby、Jellyfin 或 Plex，直接连接。',
    guide_nav2_tag: '情况二',
    guide_nav2_title: '直接播放文件',
    guide_nav2_desc: '跳过服务器，直接播放本机或局域网文件。',
    guide_nav3_tag: '情况三',
    guide_nav3_title: '想要媒体库',
    guide_nav3_desc: '还没有服务器？搭一个最简单的。',
    guide_nav4_tag: '延伸阅读',
    guide_nav4_title: '设备怎么选',
    guide_nav4_desc: '比较电脑、Apple TV 和本地影音播放器的取舍。',

    guide_s1_eyebrow: 'ALREADY HAVE A SERVER',
    guide_s1_title: '已经熟悉 NAS 或媒体服务器',
    guide_s1_body: '如果你的 NAS 或电脑上已经在运行 Emby、Jellyfin 或 Plex，不需要额外准备：打开 TinyPlay，在“添加内容源”中选择对应类型，填入地址、端口与账号即可。媒体库会照常以海报墙形式呈现，选集、搜索、最近观看与续播都会正常工作。',
    guide_s1_callout_title: '国产 NAS 用户请注意',
    guide_s1_callout_body: '如果你使用的是<b>极空间</b>，其自带的媒体服务功能兼容 Emby 接口协议，添加内容源时选择「Emby」类型即可。',

    guide_s2_eyebrow: 'PLAY FILES DIRECTLY',
    guide_s2_title: '不需要媒体库，直接播放文件',
    guide_s2_body: '如果你暂时不打算搭建媒体服务器，可以直接添加本地文件夹、已挂载目录、SMB 或 WebDAV 共享。连通后，用手机浏览并选择要作为起点的文件夹；之后就能按目录浏览和播放。遥控器照常支持暂停、进度、字幕、音轨和倍速，只是没有海报、剧集信息与续播记录。',
    guide_os_mac: 'macOS',
    guide_os_mac_local_title: '播放本机目录',
    guide_os_mac_local_body: '选择「本地文件夹」，再在目录选择器里浏览这台 Mac 上的文件夹；也可以先用「访达 → 前往 → 连接服务器」挂载 NAS 共享后再选择它。',
    guide_os_mac_smb_title: '直接播放 NAS 共享',
    guide_os_mac_smb_body: '选择「SMB 共享」，填写共享地址与账号密码；连接成功后，直接在 TinyPlay 中选择要浏览的共享和文件夹。',
    guide_os_win: 'Windows',
    guide_os_win_local_title: '播放本机目录',
    guide_os_win_local_body: '选择「本地文件夹」，再在目录选择器里浏览任意磁盘；也可以先在“此电脑”中映射网络驱动器，再选择映射后的目录。',
    guide_os_win_smb_title: '直接播放 NAS 共享',
    guide_os_win_smb_body: '选择「SMB 共享」，填写共享地址与账号密码；连接成功后，在 TinyPlay 中选择共享和起始文件夹，无需先映射驱动器。',

    guide_s3_eyebrow: 'WANT A MEDIA LIBRARY',
    guide_s3_title: '想要媒体库体验，但还没有服务器',
    guide_s3_body: '如果你想要完整的媒体库海报墙、剧集选集与续播体验，最简单的办法是先搭建一个媒体服务器，再用 TinyPlay 连接它。',
    guide_wall_image_alt: '媒体库海报墙概念示意：电影和剧集封面以网格方式排列',
    guide_wall_label: '先弄清一个词：影音墙',
    guide_wall_title: '把电影、剧集和继续观看，按封面整齐排在一起。',
    guide_wall_body: '它不是播放文件的必需条件：直接浏览文件夹更快。但接入 Emby、Jellyfin 或 Plex 后，TinyPlay 可以按封面浏览内容，并提供剧集、搜索和继续观看等信息。',
    guide_wall_note: '概念示意图；实际界面以产品截图为准。',
    guide_reco_tag: '中文用户推荐',
    guide_reco_title: 'Jellyfin',
    guide_reco_body: '完全免费开源，官方提供一条命令即可运行的 Docker 镜像，中文资料与社区齐全，且无需注册海外账号，在国内网络环境下也能正常使用。群晖、威联通、极空间等主流 NAS 系统大多也已提供一键安装的 Jellyfin 套件。搭建完成后，在 TinyPlay 中选择「Jellyfin」类型添加内容源即可。',

    guide_cta_title: '准备好了吗？',
    guide_cta_body: '下载 TinyPlay，几分钟内就能把那台小主机接管电视。',
    guide_cta_button: '下载 macOS / Windows',
  };
  messages[EN] = {
    page_title: 'TinyPlay — squeeze out your mini PC, turn it into a living-room player',
    meta_description: 'TinyPlay turns the Windows PC or Mac connected to your TV into a phone-controlled living-room player.',
    og_title: 'TinyPlay — Your phone is the remote. Your computer does the playing.',
    og_description: 'Turn the computer connected to your TV into a phone-controlled living-room player.',
    nav_why: 'Why',
    nav_features: 'Features',
    nav_choose: 'Buying guide',
    nav_guide: 'Getting started',
    nav_appletv: 'Apple TV coming soon',
    nav_download: 'Download',
    hero_eyebrow: 'Phone control, computer playback',
    hero_title: 'Your phone is the remote.<br><em>The computer does the playing.</em>',
    hero_lead: 'Let the Windows PC or Mac connected to your TV handle playback. Pick a title, scrub, and switch subtitles from your phone — no phone app required, all on your home LAN.',
    btn_download_app: 'Download macOS / Windows <span>↗</span>',
    btn_view_appletv: 'Apple TV · coming soon',
    platform_win: 'Windows x86-64',
    platform_mac: 'macOS Apple Silicon / Intel',
    platform_phone: 'No app needed on your phone',
    signal_lan: 'Connected on LAN',
    signal_room: 'Living-room player',
    why_title: 'The computer you already have<br>can be a great TV player too.',
    why1_title: 'You may not need new hardware',
    why1_body: 'An always-on x86 mini PC (such as a NUC), Mac, or even an unused Windows laptop may be enough. If it can output reliably and decode your media, connect its HDMI port to the TV.',
    why2_title: '"Can play" isn’t "easy to control"',
    why2_body: 'Plugging a computer into the TV is the easy part. The annoying part is picking a title, scrubbing, switching subtitles from the couch: a keyboard and mouse don’t belong in the living room, and remote desktop is overkill.',
    why3_title: 'So we built TinyPlay',
    why3_body: 'Picking a title, scrubbing, switching subtitles and speed all move to your phone. The computer connects to a library, folders, or a live-TV source, then hands playback to the bundled mpv. You do not need to know what mpv is — just let the computer handle format compatibility.',
    features_eyebrow: 'FOUR ENTRANCES, ONE REMOTE',
    features_title: 'Four ways in.<br>One phone remote.',
    features_lead: 'No matter where your media lives, playback stays on the computer connected to the TV; browsing, choosing, and controlling happen on your phone.',
    feature1_title: 'Media libraries',
    feature1_body: 'Connect Emby, Jellyfin, or Plex for posters, episodes, and continue watching.',
    feature2_title: 'Folders',
    feature2_body: 'Browse and play from local folders, mounted paths, SMB, or WebDAV.',
    feature3_title: 'IPTV',
    feature3_body: 'Import an existing M3U/M3U8 playlist and add XMLTV for programme information.',
    feature4_title: 'DLNA casting',
    feature4_body: 'Let DLNA-capable apps on your LAN cast directly to this computer.',
    product_note: 'Pause, seek, speed, audio, subtitles, and picture controls always use the same remote. Playback is handled by bundled mpv.',
    hero_image_alt: 'A phone controlling content played by a computer connected to a TV',
    remote_image_alt: 'TinyPlay phone remote interface',
    library_image_alt: 'TinyPlay phone media-library interface',
    flow_title: 'Three steps to put that HDMI port to work.',
    flow1_title: 'Install',
    flow1_body: 'Run TinyPlay on your Windows mini PC or Mac.',
    flow2_title: 'Connect',
    flow2_body: 'Add a media server or network share, and plug the computer into your TV over HDMI.',
    flow3_title: 'Play',
    flow3_body: 'Scan the QR code on your phone, pick something, and sit back down.',
    choose_title: 'Which box actually belongs in your TV stand?',
    choose_lead: 'There is no single winner, only different trade-offs. Start with what you already own and what you use it for before buying another device.',
    t_dimension: 'Dimension',
    t_col_nuc: 'Windows mini PC / laptop',
    t_col_mac: 'M-series Mac mini',
    t_col_atv: 'Apple TV 4K',
    t_col_bluray: 'Local-media player (e.g. Zidoo)',
    t_row_position: 'Core role',
    t_position_nuc: 'Configurable computer player',
    t_position_mac: 'Computer + player',
    t_position_atv: 'Streaming box',
    t_position_bluray: 'Dedicated local-media player',
    t_row_docker: 'Downloads / Docker',
    t_docker_nuc: 'Possible',
    t_docker_mac: 'Possible',
    t_docker_atv: 'Not suited',
    t_docker_bluray: 'Not supported',
    t_row_nas: 'Local media / NAS',
    t_nas_nuc: 'Wide software and format choice',
    t_nas_mac: 'Wide software choice',
    t_nas_atv: 'Usually needs a third-party app',
    t_nas_bluray: 'Built for local playback',
    t_row_streaming: 'Streaming',
    t_streaming_nuc: 'Browser & desktop apps',
    t_streaming_mac: 'Browser & desktop apps',
    t_streaming_atv: 'Works out of the box',
    t_streaming_bluray: 'Depends on the apps',
    t_row_hd_audio: 'HD audio passthrough',
    t_hd_audio_nuc: 'Depends on hardware, OS, and setup',
    t_hd_audio_mac: 'Not known for bitstreaming',
    t_hd_audio_atv: 'Not known for local HD passthrough',
    t_hd_audio_bluray: 'Depends on model and format',
    t_row_hdr: 'HDR / Dolby Vision',
    t_hdr_nuc: 'Depends on GPU, OS, and player',
    t_hdr_mac: 'HDR support varies with the media',
    t_hdr_atv: 'Convenient for streaming',
    t_hdr_bluray: 'Local-media experience varies by model',
    t_row_maintenance: 'Upkeep',
    t_maintenance_nuc: 'Medium to high',
    t_maintenance_mac: 'Medium',
    t_maintenance_atv: 'Low',
    t_maintenance_bluray: 'Low to medium',
    t_row_best_for: 'Best for',
    t_best_nuc: 'You already have a computer and want flexible setup',
    t_best_mac: 'You want the TV stand to also hold a full computer',
    t_best_atv: 'Streaming-first, low-maintenance for the whole family',
    t_best_bluray: 'Local media first, with a dedicated device',
    v1_tag: 'Streaming first',
    v1_body: 'For people who mostly stream and want the whole household to get started easily.',
    v2_tag: 'Already own a computer',
    v2_title: 'Windows mini PC / laptop',
    v2_body: 'If you already have a computer that can connect to the TV, TinyPlay is one way to turn it into a living-room player.',
    v3_tag: 'Everyday computing too',
    v3_title: 'M-series Mac mini',
    v3_body: 'Playback, browsing, work, and home services together — for people who want to keep a full computer.',
    v4_tag: 'Local media first',
    v4_title: 'Local-media player (e.g. Zidoo)',
    v4_body: 'For people whose main use is local-media playback and who prefer a dedicated device.',
    guide_note: 'Actual HDR, Dolby Vision, and audio capabilities vary by chip, OS, driver, player, source container, and AV equipment. Check your exact hardware’s specs before buying.',
    download_title: 'Let that mini PC<br>take over the TV tonight.',
    download_platforms: 'Windows x86-64 · macOS Apple Silicon / Intel',
    footer_tagline: 'Turn the little box into the big screen.',
    footer_license: 'GPL-3.0 License',
    appletv_modal_title: 'Apple TV version',
    appletv_modal_body: 'In development — stay tuned.',
    appletv_modal_close: 'Got it',

    guide_page_title: 'Getting Started — TinyPlay',
    guide_meta_description: 'A TinyPlay getting-started guide for existing media servers, direct folder playback, and setting up a media library.',
    guide_back_home: '← Back to home',
    guide_eyebrow: 'GETTING STARTED',
    guide_title: 'Start here — find the setup that fits you',
    guide_lead: 'Whether you already use a NAS or only want to play one folder from your computer, TinyPlay can be running quickly. Start with the situation closest to yours.',

    guide_nav1_tag: 'Situation one',
    guide_nav1_title: 'Already have a media server',
    guide_nav1_desc: 'Running Emby, Jellyfin, or Plex already — just connect.',
    guide_nav2_tag: 'Situation two',
    guide_nav2_title: 'Play files directly',
    guide_nav2_desc: 'Skip the server and play local or LAN files directly.',
    guide_nav3_tag: 'Situation three',
    guide_nav3_title: 'Want a media library',
    guide_nav3_desc: 'No server yet? Stand up the simplest one.',
    guide_nav4_tag: 'Further reading',
    guide_nav4_title: 'Which device fits?',
    guide_nav4_desc: 'Compare computers, Apple TV, and local-media players.',

    guide_s1_eyebrow: 'ALREADY HAVE A SERVER',
    guide_s1_title: 'Already comfortable with a NAS or media server',
    guide_s1_body: 'If Emby, Jellyfin, or Plex is already running on your NAS or computer, there’s nothing extra to prepare: open TinyPlay, choose the matching type under “Add Media Source,” and enter the address, port, and credentials. Your library appears as the familiar poster wall, with episodes, search, resume, and recently-watched all working as expected.',
    guide_s1_callout_title: 'Note for Chinese NAS users',
    guide_s1_callout_body: 'For 极空间, use the <b>Emby</b> type.',

    guide_s2_eyebrow: 'PLAY FILES DIRECTLY',
    guide_s2_title: 'No media library needed — just play files',
    guide_s2_body: 'If you are not ready to run a media server, add a local folder, mounted path, SMB share, or WebDAV share directly. Once connected, browse from your phone and choose the folder to start from; then browse and play by folder. The remote still gives you pause, seek, subtitles, audio tracks, and speed — you simply will not get posters, episode metadata, or resume history.',
    guide_os_mac: 'macOS',
    guide_os_mac_local_title: 'Playing a folder on this Mac',
    guide_os_mac_local_body: 'Choose “Local folder”, then browse this Mac in the folder picker. You can also mount a NAS share first with Finder → Go → Connect to Server, then choose it there.',
    guide_os_mac_smb_title: 'Playing a NAS share directly',
    guide_os_mac_smb_body: 'Choose “SMB share”, enter the share address and credentials, then choose the share and folder inside TinyPlay after it connects.',
    guide_os_win: 'Windows',
    guide_os_win_local_title: 'Playing a folder on this PC',
    guide_os_win_local_body: 'Choose “Local folder”, then browse any drive in the folder picker. You can also map a NAS share in This PC first, then choose the mapped folder.',
    guide_os_win_smb_title: 'Playing a NAS share directly',
    guide_os_win_smb_body: 'Choose “SMB share”, enter the share address and credentials, then choose the share and starting folder inside TinyPlay — no drive mapping required.',

    guide_s3_eyebrow: 'WANT A MEDIA LIBRARY',
    guide_s3_title: 'Want a media library, but do not have a server yet',
    guide_s3_body: 'For a library with posters, episode browsing, and resume, the simplest route is to stand up a media server first, then point TinyPlay at it.',
    guide_wall_image_alt: 'A media-library poster-wall concept with films and TV shows arranged in a grid',
    guide_wall_label: 'First, one term: a poster wall',
    guide_wall_title: 'Your films, episodes, and continue-watching queue, organised by cover art.',
    guide_wall_body: 'It is not required to play files — browsing a folder is faster. But when you connect Emby, Jellyfin, or Plex, TinyPlay can present content by cover art with episode browsing, search, and continue-watching information.',
    guide_wall_note: 'Concept illustration; see product screenshots for the actual interface.',
    guide_reco_tag: 'Recommended',
    guide_reco_title: 'Plex',
    guide_reco_body: 'The most widely supported option in North America, with a mature phone and TV app ecosystem and a one-line Docker image to get started. TinyPlay’s own player handles the actual playback, so a Plex Pass isn’t required for anything TinyPlay uses. Once it’s running, add it in TinyPlay as a “Plex” type server.',

    guide_cta_title: 'Ready when you are.',
    guide_cta_body: 'Download TinyPlay and let that mini PC take over the TV in minutes.',
    guide_cta_button: 'Download macOS / Windows',
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
    document.querySelectorAll('[data-lang-only]').forEach(function (el) {
      el.classList.toggle('hidden', el.getAttribute('data-lang-only') !== lang);
    });
  }

  function toggleSiteLang() {
    var current = document.documentElement.lang === ZH ? ZH : EN;
    var next = current === ZH ? EN : ZH;
    try { localStorage.setItem(STORAGE_KEY, next); } catch (e) {}
    applyLang(next);
  }
  window.toggleSiteLang = toggleSiteLang;

  var lastFocusedElement = null;

  function openAppleTvModal(event) {
    if (event) event.preventDefault();
    lastFocusedElement = document.activeElement;
    document.body.classList.add('modal-open');
    document.getElementById('appletv-modal-backdrop').classList.remove('hidden');
    document.getElementById('appletv-modal-close').focus();
  }
  window.openAppleTvModal = openAppleTvModal;

  function closeAppleTvModal() {
    document.getElementById('appletv-modal-backdrop').classList.add('hidden');
    document.body.classList.remove('modal-open');
    if (lastFocusedElement && typeof lastFocusedElement.focus === 'function') lastFocusedElement.focus();
    lastFocusedElement = null;
  }
  window.closeAppleTvModal = closeAppleTvModal;

  function onAppleTvBackdropClick(event) {
    if (event.target.id === 'appletv-modal-backdrop') closeAppleTvModal();
  }
  window.onAppleTvBackdropClick = onAppleTvBackdropClick;

  document.addEventListener('keydown', function (event) {
    var backdrop = document.getElementById('appletv-modal-backdrop');
    if (!backdrop || backdrop.classList.contains('hidden')) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      closeAppleTvModal();
      return;
    }
    if (event.key === 'Tab') {
      event.preventDefault();
      document.getElementById('appletv-modal-close').focus();
    }
  });

  document.addEventListener('DOMContentLoaded', function () {
    applyLang(detectLang());
  });
})();
