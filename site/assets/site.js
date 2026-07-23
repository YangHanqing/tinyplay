(function () {
  var ZH = 'zh-CN';
  var EN = 'en';

  var messages = {};
  messages[ZH] = {
    page_title: 'TinyPlay — 闲置 PC，接管电视',
    meta_description: 'TinyPlay：让闲置 Windows PC 或 Mac 接上电视，用手机控制 Jellyfin、NAS 文件、IPTV 和支持的网站播放。',
    og_title: 'TinyPlay — 闲置 PC，接管电视',
    og_description: '手机遥控，电脑接电视播放。Jellyfin、NAS 文件和 IPTV 都能接进来。',
    nav_why: '为什么',
    nav_features: '功能',
    nav_choose: '选购指南',
    nav_guide: '使用指南',
    guide_path: 'guide/',
    nav_appletv: 'Apple TV 即将推出',
    nav_download: '下载 <span class="nav-platform">macOS / Windows</span><span class="free-badge">免费</span>',
    hero_eyebrow: '把小主机变成客厅播放器 HTPC',
    hero_title: '可能比<em>电视盒子</em><br>更好用',
    hero_lead: '把闲置的 Windows 小主机或 Mac mini 接上电视。手机扫码即可选电影、拖进度、切字幕、调倍速。没有复杂的插件系统和电视端菜单。打开手机，找到想看的，剩下的交给接电视的电脑。',
    btn_download_app: '下载 macOS / Windows <span class="free-badge">免费</span><span>↗</span>',
    btn_read_guide: '使用指南',
    btn_view_appletv: 'Apple TV 版 · 即将推出',
    platform_win: 'Windows 10+',
    platform_mac: 'macOS 13+',
    platform_phone: '手机无需安装 App',
    hero_tags_label: '支持的内容入口',
    hero_tag_media_servers: 'Jellyfin / Emby / Plex',
    hero_tag_local_files: '本地文件 / NFS',
    hero_tag_network_shares: 'SMB / WebDAV',
    hero_tag_iptv: 'IPTV',
    hero_tag_dlna: 'DLNA',
    hero_tag_chinese_video: 'B站 / 爱 / 优 / 腾',
    signal_lan: '局域网已连接',
    signal_room: '客厅播放器',
    why_title: '客厅不该在自由和好用之间<br>二选一。',
    why1_title: '电视盒子很省心，但不一定够自由',
    why1_body: '想用电脑端软件、NAS、媒体服务器或自己的直播源时，电视盒子往往又多了一层限制。',
    why2_title: '小主机很自由，但不该靠键盘鼠标',
    why2_body: '电脑接电视能做很多事，问题是它原本不是为沙发上的选片、搜索和拖进度设计的。',
    why3_title: 'TinyPlay 补上中间这一环',
    why3_body: '手机给你现代、顺手的控制界面；小主机继续负责内容、播放和格式兼容。',
    feature_content_eyebrow: '内容入口，都在手机里',
    feature_content_title: '媒体库 & IPTV',
    feature_content_desc: '从 Emby、Jellyfin、Plex 和文件夹中找片，或导入 M3U/M3U8 直播频道。选好以后，手机接着就是遥控器。',
    content_showcase_label: 'TinyPlay 手机界面预览',
    content_tabs_label: '选择界面预览',
    content_tab_library: '媒体库',
    content_tab_iptv: 'IPTV',
    content_tab_remote: '遥控器',
    feature_web_eyebrow: '手机控制在线视频',
    feature_web_beta: '中文 Beta',
    feature_web_title: '在线视频',
    feature_web_desc: '创新的手机 / 电脑 Vim 联动：不用鼠标，也能快速选视频、控制播放、调倍速。',
    feature_web_note: '* 仍需在电脑端登录账号；能看什么、能不能跳广告，取决于平台和你的会员。',
    feature_dlna_eyebrow: '接收局域网投屏',
    feature_dlna_title: 'DLNA 接收器',
    feature_dlna_desc: '让这台接电视的电脑，出现在局域网应用的投屏设备列表里。',
    feature_placeholder_label: '素材待补',
    feature_web_placeholder: '在线视频控制界面',
    feature_iptv_placeholder: 'IPTV 频道界面',
    feature_dlna_placeholder: 'DLNA 投屏界面',
    iptv_image_alt: 'TinyPlay 手机上的 IPTV 频道界面',
    dlna_image_alt: '手机选择 TinyPlay 作为 DLNA 接收器',
    online_video_image_alt: '手机控制电脑端在线视频播放',
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
    download_platforms: 'Windows 10+ / macOS 13+',
    download_atv_title: 'Apple TV 版',
    download_atv_badge: '即将推出',
    footer_tagline: 'Turn the little box into the big screen.',
    footer_license: 'GPL-3.0 License',
    appletv_modal_title: 'Apple TV 版本',
    appletv_modal_body: '原生 Apple TV 版本正在准备中，上线后可直接在 App Store 获取。',
    appletv_modal_close: '好的',

    guide_page_title: '使用指南 — TinyPlay',
    guide_meta_description: 'TinyPlay 使用指南：已有媒体服务器、直接播放文件，或搭建一个媒体库，从适合你的方式开始。',
    guide_back_home: '← 返回首页',
    guide_doc_label: 'TINYPLAY 文档',
    guide_toc_title: '目录',
    guide_eyebrow: 'GETTING STARTED',
    guide_title: '使用指南',
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
    page_title: 'TinyPlay — Let an idle PC take over the TV',
    meta_description: 'TinyPlay turns an idle Windows PC or Mac into a phone-controlled TV player for Jellyfin, NAS folders, IPTV, and more.',
    og_title: 'TinyPlay — Let an idle PC take over the TV',
    og_description: 'Control the computer connected to your TV from your phone. Bring Jellyfin, NAS folders, and IPTV together.',
    nav_why: 'Why',
    nav_features: 'Features',
    nav_choose: 'Buying guide',
    nav_guide: 'Getting started',
    guide_path: 'guide/en/',
    nav_appletv: 'Apple TV coming soon',
    nav_download: 'Download <span class="nav-platform">macOS / Windows</span><span class="free-badge">FREE</span>',
    hero_eyebrow: 'TURN A MINI PC INTO AN HTPC',
    hero_title: 'Maybe better than<br><em>a streaming box.</em>',
    hero_lead: 'Connect a spare Windows PC or Mac mini to your TV. Scan the QR code to browse, seek, switch subtitles, and change playback speed. No plugin maze, no TV-first menus. Find what you want on your phone and let the PC handle playback.',
    btn_download_app: 'Download macOS / Windows <span class="free-badge">FREE</span><span>↗</span>',
    btn_read_guide: 'Getting started',
    btn_view_appletv: 'Apple TV · coming soon',
    platform_win: 'Windows 10+',
    platform_mac: 'macOS 13+',
    platform_phone: 'No app needed on your phone',
    hero_tags_label: 'Supported content entry points',
    hero_tag_media_servers: 'Jellyfin / Emby / Plex',
    hero_tag_local_files: 'Local files / NFS',
    hero_tag_network_shares: 'SMB / WebDAV',
    hero_tag_iptv: 'IPTV',
    hero_tag_dlna: 'DLNA',
    hero_tag_chinese_video: 'Chinese online video',
    signal_lan: 'Connected on LAN',
    signal_room: 'Living-room player',
    why_title: 'The living room should not make you choose<br>between freedom and ease.',
    why1_title: 'TV boxes are simple, but not always flexible',
    why1_body: 'When you want desktop software, NAS folders, media servers, or your own live sources, a TV box can become one limitation too many.',
    why2_title: 'Mini PCs are flexible, but should not need a keyboard and mouse',
    why2_body: 'A computer connected to the TV can do a lot. It just was not designed to browse, search, and scrub from the couch.',
    why3_title: 'TinyPlay fills the gap',
    why3_body: 'Your phone gives you a modern, familiar control surface. Your mini PC keeps handling content, playback, and format compatibility.',
    feature_content_eyebrow: 'YOUR CONTENT, ONE PHONE',
    feature_content_title: 'Library & IPTV',
    feature_content_desc: 'Find something through Emby, Jellyfin, Plex, or your folders, or bring in live channels with an M3U/M3U8 playlist. Once you choose, your phone becomes the remote.',
    content_showcase_label: 'TinyPlay phone interface preview',
    content_tabs_label: 'Choose an interface preview',
    content_tab_library: 'Library',
    content_tab_iptv: 'IPTV',
    content_tab_remote: 'Remote',
    feature_web_eyebrow: 'CONTROL ONLINE VIDEO FROM YOUR PHONE',
    feature_web_beta: 'Chinese beta',
    feature_web_title: 'Online video',
    feature_web_desc: 'An innovative Vim-style link between your phone and computer: pick videos, control playback, and change speed without reaching for the mouse.',
    feature_web_note: '* You still sign in on the computer. What you can watch and whether ads can be skipped depend on the platform and your subscription.',
    feature_dlna_eyebrow: 'RECEIVE LAN CASTING',
    feature_dlna_title: 'DLNA receiver',
    feature_dlna_desc: 'Make the computer connected to your TV appear in the casting list of DLNA-capable apps on your LAN.',
    feature_placeholder_label: 'ARTWORK TO COME',
    feature_web_placeholder: 'Online-video control interface',
    feature_iptv_placeholder: 'IPTV channel interface',
    feature_dlna_placeholder: 'DLNA casting interface',
    iptv_image_alt: 'TinyPlay IPTV channel interface on a phone',
    dlna_image_alt: 'A phone selecting TinyPlay as a DLNA receiver',
    online_video_image_alt: 'A phone controlling online video on a computer',
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
    download_platforms: 'Windows 10+ / macOS 13+',
    download_atv_title: 'Apple TV edition',
    download_atv_badge: 'COMING SOON',
    footer_tagline: 'Turn the little box into the big screen.',
    footer_license: 'GPL-3.0 License',
    appletv_modal_title: 'Apple TV version',
    appletv_modal_body: 'The native Apple TV edition is in the works and will be available from the App Store.',
    appletv_modal_close: 'Got it',

    guide_page_title: 'Getting Started — TinyPlay',
    guide_meta_description: 'A TinyPlay getting-started guide for existing media servers, direct folder playback, and setting up a media library.',
    guide_back_home: '← Back to home',
    guide_doc_label: 'TINYPLAY DOCS',
    guide_toc_title: 'Contents',
    guide_eyebrow: 'GETTING STARTED',
    guide_title: 'Getting started',
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
    document.querySelectorAll('.content-showcase').forEach(function (showcase) {
      var section = showcase.closest('.feature-section--content');
      var buttons = section ? section.querySelectorAll('[data-content-screen]') : [];

      function setActiveScreen(screen) {
        showcase.setAttribute('data-active-screen', screen);
        buttons.forEach(function (item) {
          item.setAttribute('aria-pressed', item.getAttribute('data-content-screen') === screen ? 'true' : 'false');
        });
      }

      buttons.forEach(function (button) {
        button.addEventListener('click', function () {
          setActiveScreen(button.getAttribute('data-content-screen'));
        });
      });

      function updateOnScroll() {
        if (!section || window.innerWidth <= 900) return;
        var rect = section.getBoundingClientRect();
        var scrollRange = Math.max(1, section.offsetHeight - window.innerHeight);
        var progress = Math.min(1, Math.max(0, -rect.top / scrollRange));
        var screens = ['library', 'iptv', 'remote'];
        setActiveScreen(screens[Math.min(screens.length - 1, Math.floor(progress * screens.length))]);
      }

      var scrollQueued = false;
      window.addEventListener('scroll', function () {
        if (scrollQueued) return;
        scrollQueued = true;
        window.requestAnimationFrame(function () {
          scrollQueued = false;
          updateOnScroll();
        });
      }, { passive: true });
      window.addEventListener('resize', updateOnScroll);
      updateOnScroll();
    });
  });
})();
