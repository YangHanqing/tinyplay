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
    nav_guide: '使用指南',
    nav_appletv: '查看 Apple TV',
    nav_download: '下载',
    hero_eyebrow: '手机控制，电脑播放',
    hero_title: '手机是遥控器，<br><em>电脑负责播放。</em>',
    hero_lead: '接上电视的电脑负责真正的播放，你窝在沙发上用手机选片、拖进度条、调倍速——不用再摸键盘鼠标，也不用满沙发缝找遥控器。那台电脑正好可以是家里已经在跑 Docker 的 NUC、迷你主机，或一台 Mac mini。',
    btn_download_app: '下载 macOS / Windows <span>↗</span>',
    btn_view_appletv: '查看 Apple TV',
    platform_win: 'Windows x86-64',
    platform_mac: 'macOS Apple Silicon / Intel',
    platform_phone: '手机无需安装 App',
    signal_lan: '局域网已连接',
    signal_room: '客厅播放器',
    why_title: '那台 24 小时开机的小主机，<br>其实离电视播放器只差一步。',
    why1_title: '性能已经在那里了',
    why1_body: '很多人家里那台 NUC 或迷你主机——哪怕只是台 N100——本来就在做下载机、跑 Docker、管理 NAS。它功耗不高、长期在线，还有一个经常闲着的 HDMI 接口。没必要再买一台只能做一件事的设备。',
    why2_title: '但"能播放"不等于"好控制"',
    why2_body: '电脑接上电视很简单，真正麻烦的是窝在沙发里选片、拖进度、切字幕：键盘鼠标不属于客厅，远程桌面又太重。',
    why3_title: '于是有了 TinyPlay',
    why3_body: '选片、拖进度条、切换倍速——这些你会反复动手的操作，全部挪到手机上完成。桌面端连接 Emby、Jellyfin、Plex、IPTV 直播源或 SMB/WebDAV 共享并驱动内置的万能播放器，手机扫码即可浏览媒体库或频道列表。播放留在电脑，控制回到你手里。',
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
    feature5_title: 'IPTV 直播频道',
    feature5_body: '导入 M3U/M3U8 播放列表即可看频道列表，配合 XMLTV 节目单显示正在播出的节目，支持收藏与最近观看。',
    flow_title: '三步，把 HDMI 用起来。',
    flow1_title: '安装',
    flow1_body: '在 Windows 小主机或 Mac 上运行 TinyPlay。',
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
    download_platforms: 'Windows x86-64 · macOS Apple Silicon / Intel',
    footer_tagline: 'Turn the little box into the big screen.',
    footer_license: 'MIT License',
    appletv_modal_title: 'Apple TV 版本',
    appletv_modal_body: '正在开发中，敬请期待。',
    appletv_modal_close: '好的',

    guide_page_title: '使用指南 — TinyPlay',
    guide_meta_description: 'TinyPlay 使用指南：根据你是否已有媒体服务器、是否需要海报墙体验，选择最适合你的接入方式。',
    guide_back_home: '← 返回首页',
    guide_eyebrow: 'GETTING STARTED',
    guide_title: '三条路径，找到最适合你的起步方式',
    guide_lead: '无论你是已经在跑 NAS 的老玩家，还是第一次接触家庭媒体服务器，都能在几分钟内看到 TinyPlay 真正跑起来的样子。下面按你目前的情况分成几条路径，挑一条对应你的即可，彼此互不冲突，也可以都试试。',

    guide_nav1_tag: '路径一',
    guide_nav1_title: '已有 NAS 或服务器',
    guide_nav1_desc: '已经在用 Emby、Jellyfin 或 Plex，直接连接。',
    guide_nav2_tag: '路径二',
    guide_nav2_title: '不在意海报墙',
    guide_nav2_desc: '跳过服务器，直接播放本地或局域网文件。',
    guide_nav3_tag: '路径三',
    guide_nav3_title: '想要海报墙',
    guide_nav3_desc: '还没有服务器？搭一个最简单的。',
    guide_nav4_tag: '路径四 · 可选',
    guide_nav4_title: '只想先看效果',
    guide_nav4_desc: '不装任何服务器，用一条直播源试试。',

    guide_s1_eyebrow: 'PATH ONE',
    guide_s1_title: '已经熟悉 NAS 或媒体服务器',
    guide_s1_body: '如果你的 NAS 或电脑上已经在运行 Emby、Jellyfin 或 Plex，不需要额外准备：打开 TinyPlay，在"添加服务器"中选择对应类型，填入地址、端口与账号即可。媒体库会照常以海报墙形式呈现，选集、搜索、最近观看与续播都会正常工作。',
    guide_s1_callout_title: '国产 NAS 用户请注意',
    guide_s1_callout_body: '如果你使用的是<b>极空间</b>，其自带的媒体服务功能兼容 Emby 接口协议，添加服务器时选择「Emby」类型即可；如果是<b>飞牛影视（fnOS）</b>，则兼容 Jellyfin 接口协议，选择「Jellyfin」类型即可。这是两款国内常见的第三方 NAS 系统，如果你还不了解，可自行搜索了解详情。',

    guide_s2_eyebrow: 'PATH TWO',
    guide_s2_title: '不需要海报墙，直接播放本地文件',
    guide_s2_body: '如果你暂时不打算搭建媒体服务器，也不在意海报墙式的浏览体验，可以直接把 TinyPlay 指向一个文件夹或网络共享——添加服务器时选择「文件」类型，用手机以面包屑目录的方式浏览并点选播放。暂停、进度、字幕、音轨、倍速等播放控制与连接服务器时完全一致，只是没有海报、剧集信息与续播记录。',
    guide_os_mac: 'macOS',
    guide_os_mac_local_title: '播放本机目录',
    guide_os_mac_local_body: '协议选择「本机 / 已挂载路径」，路径直接填 Finder 里的文件夹（如 /Users/xxx/Movies）；也可以先用「访达 → 前往 → 连接服务器」把 NAS 共享挂载好，再按本机路径浏览。',
    guide_os_mac_smb_title: '直接播放 NAS 共享',
    guide_os_mac_smb_body: '协议选择「SMB 共享」，无需提前挂载，直接填共享地址、账号与密码即可。',
    guide_os_win: 'Windows',
    guide_os_win_local_title: '播放本机目录',
    guide_os_win_local_body: '协议选择「本机 / 已挂载路径」，路径可以是任意本地盘符（如 D:\\Movies）；也可以先在"此电脑"里"映射网络驱动器"，把 NAS 共享映射成一个盘符后再按本机路径浏览。',
    guide_os_win_smb_title: '直接播放 NAS 共享',
    guide_os_win_smb_body: '协议同样选择「SMB 共享」，直接填共享地址、账号与密码，无需先映射驱动器。',

    guide_s3_eyebrow: 'PATH THREE',
    guide_s3_title: '想要海报墙体验，但还没有服务器',
    guide_s3_body: '如果你想要完整的媒体库海报墙、剧集选集与续播体验，最简单的办法是先搭建一个媒体服务器，再用 TinyPlay 连接它。',
    guide_reco_tag: '中文用户推荐',
    guide_reco_title: 'Jellyfin',
    guide_reco_body: '完全免费开源，官方提供一条命令即可运行的 Docker 镜像，中文资料与社区齐全，且无需注册海外账号，在国内网络环境下也能正常使用。群晖、威联通、极空间、飞牛等主流 NAS 系统大多也已提供一键安装的 Jellyfin 套件。搭建完成后，在 TinyPlay 中选择「Jellyfin」类型添加服务器即可。',

    guide_s4_eyebrow: 'PATH FOUR · OPTIONAL',
    guide_s4_title: '只想先看效果，试试电视直播',
    guide_s4_body: '如果你手头有一个 M3U / M3U8 播放列表地址（例如自有的电视直播源），添加服务器时选择「IPTV」类型，粘贴播放列表地址即可看到频道列表；如果同时有节目单（XMLTV）地址，还能看到每个频道正在播出的节目。这是最快看到 TinyPlay 播放效果的方式，不需要任何服务器。',

    guide_cta_title: '准备好了吗？',
    guide_cta_body: '下载 TinyPlay，几分钟内就能把那台小主机接管电视。',
    guide_cta_button: '下载 macOS / Windows',
  };
  messages[EN] = {
    page_title: 'TinyPlay — squeeze out your mini PC, turn it into a living-room player',
    meta_description: 'TinyPlay turns the NUC, mini PC, or Mac mini already running in your home into a phone-controlled living-room player.',
    nav_why: 'Why',
    nav_features: 'Features',
    nav_choose: 'Buying guide',
    nav_guide: 'Getting started',
    nav_appletv: 'View Apple TV',
    nav_download: 'Download',
    hero_eyebrow: 'Phone control, computer playback',
    hero_title: 'Your phone is the remote.<br><em>The computer does the playing.</em>',
    hero_lead: 'The computer already hooked up to your TV handles the actual playback. From the couch, pick a title, scrub the progress bar, and change the speed — all from your phone, no remote-hunting or keyboard-and-mouse required. That computer can just as well be the NUC or mini PC already running Docker in the background, or a Mac mini.',
    btn_download_app: 'Download macOS / Windows <span>↗</span>',
    btn_view_appletv: 'View Apple TV',
    platform_win: 'Windows x86-64',
    platform_mac: 'macOS Apple Silicon / Intel',
    platform_phone: 'No app needed on your phone',
    signal_lan: 'Connected on LAN',
    signal_room: 'Living-room player',
    why_title: 'That mini PC that’s already on 24/7<br>is one step away from a TV player.',
    why1_title: 'The performance is already there',
    why1_body: 'Plenty of people already have a NUC or mini PC at home — even a humble N100 — quietly downloading, running Docker, managing a NAS. Low power draw, always on, and usually an idle HDMI port. No need to buy another single-purpose box.',
    why2_title: '"Can play" isn’t "easy to control"',
    why2_body: 'Plugging a computer into the TV is the easy part. The annoying part is picking a title, scrubbing, switching subtitles from the couch: a keyboard and mouse don’t belong in the living room, and remote desktop is overkill.',
    why3_title: 'So we built TinyPlay',
    why3_body: 'Picking a title, scrubbing, switching playback speed — the parts you touch again and again — all move to your phone. The desktop app connects to Emby, Jellyfin, Plex, an IPTV live-TV source, or an SMB/WebDAV share and drives a built-in do-it-all player; scan a QR code to browse the library or channel list. Playback stays on the computer, control comes back to your hand.',
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
    feature5_title: 'IPTV live channels',
    feature5_body: 'Import an M3U/M3U8 playlist to get a channel list; add an XMLTV guide to see what’s currently airing. Favorites and recently-watched are supported too.',
    flow_title: 'Three steps to put that HDMI port to work.',
    flow1_title: 'Install',
    flow1_body: 'Run TinyPlay on your Windows mini PC or Mac.',
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
    download_platforms: 'Windows x86-64 · macOS Apple Silicon / Intel',
    footer_tagline: 'Turn the little box into the big screen.',
    footer_license: 'MIT License',
    appletv_modal_title: 'Apple TV version',
    appletv_modal_body: 'In development — stay tuned.',
    appletv_modal_close: 'Got it',

    guide_page_title: 'Getting Started — TinyPlay',
    guide_meta_description: 'A TinyPlay getting-started guide: pick the setup path that matches whether you already run a media server and whether you want the poster-wall experience.',
    guide_back_home: '← Back to home',
    guide_eyebrow: 'GETTING STARTED',
    guide_title: 'Four paths — find the one that fits where you’re starting from',
    guide_lead: 'Whether you’re a seasoned home-server enthusiast or setting one up for the first time, you can see TinyPlay running end-to-end within minutes. Pick the path below that matches your situation — they’re independent, so use whichever fits, or try more than one.',

    guide_nav1_tag: 'Path one',
    guide_nav1_title: 'Already have a NAS or server',
    guide_nav1_desc: 'Running Emby, Jellyfin, or Plex already — just connect.',
    guide_nav2_tag: 'Path two',
    guide_nav2_title: 'Don’t need a poster wall',
    guide_nav2_desc: 'Skip the server, play local or LAN files directly.',
    guide_nav3_tag: 'Path three',
    guide_nav3_title: 'Want the poster wall',
    guide_nav3_desc: 'No server yet? Stand up the simplest one.',
    guide_nav4_tag: 'Path four · optional',
    guide_nav4_title: 'Just want to see it work',
    guide_nav4_desc: 'No server at all — try a live-TV source.',

    guide_s1_eyebrow: 'PATH ONE',
    guide_s1_title: 'Already comfortable with a NAS or media server',
    guide_s1_body: 'If Emby, Jellyfin, or Plex is already running on your NAS or computer, there’s nothing extra to prepare: open TinyPlay, choose the matching type under “Add Server,” and enter the address, port, and credentials. Your library appears as the familiar poster wall, with episodes, search, resume, and recently-watched all working as expected.',

    guide_s2_eyebrow: 'PATH TWO',
    guide_s2_title: 'No poster wall needed — just play local files',
    guide_s2_body: 'If you’re not ready to run a media server and don’t need the poster-wall browsing experience, point TinyPlay straight at a folder or network share instead — choose the “Files” type under “Add Server” and browse a breadcrumb-style folder list from your phone. Playback controls (pause, seek, subtitles, audio tracks, speed) work exactly the same; you just won’t get posters, episode metadata, or resume history.',
    guide_os_mac: 'macOS',
    guide_os_mac_local_title: 'Playing a folder on this Mac',
    guide_os_mac_local_body: 'Choose “Local / mounted path” and point it at any folder (e.g. /Users/you/Movies) — or mount a NAS share first via Finder → Go → Connect to Server, then browse it the same way.',
    guide_os_mac_smb_title: 'Playing a NAS share directly',
    guide_os_mac_smb_body: 'Choose “SMB share” and enter the share address plus credentials — no need to mount it first.',
    guide_os_win: 'Windows',
    guide_os_win_local_title: 'Playing a folder on this PC',
    guide_os_win_local_body: 'Choose “Local / mounted path” and point it at any drive letter (e.g. D:\\Movies) — or map the NAS share to a drive letter first via This PC → Map network drive, then browse it the same way.',
    guide_os_win_smb_title: 'Playing a NAS share directly',
    guide_os_win_smb_body: 'Choose “SMB share” and enter the share address plus credentials — no drive mapping required.',

    guide_s3_eyebrow: 'PATH THREE',
    guide_s3_title: 'Want the poster wall, but don’t have a server yet',
    guide_s3_body: 'For the full poster-wall library, episode browsing, and resume experience, the simplest route is to stand up a media server first, then point TinyPlay at it.',
    guide_reco_tag: 'Recommended',
    guide_reco_title: 'Plex',
    guide_reco_body: 'The most widely supported option in North America, with a mature phone and TV app ecosystem and a one-line Docker image to get started. TinyPlay’s own player handles the actual playback, so a Plex Pass isn’t required for anything TinyPlay uses. Once it’s running, add it in TinyPlay as a “Plex” type server.',

    guide_s4_eyebrow: 'PATH FOUR · OPTIONAL',
    guide_s4_title: 'Just want to see it work — try live TV',
    guide_s4_body: 'If you already have an M3U/M3U8 playlist URL (say, an existing live-TV source), choose the “IPTV” type under “Add Server” and paste the playlist URL to see a channel list right away. Add an XMLTV guide URL too, and you’ll see what’s currently airing on each channel. It’s the fastest way to see TinyPlay play something, with no server required at all.',

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
