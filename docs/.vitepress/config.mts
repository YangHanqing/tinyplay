import { defineConfig } from 'vitepress'

const chineseSidebar = [
  {
    text: '产品使用指南',
    items: [
      { text: '首页', link: '/' },
      { text: '连接媒体服务器', link: '/media-server' },
      { text: '直接播放文件', link: '/files' },
      { text: 'IPTV', link: '/iptv' },
      { text: '接收 DLNA 投屏', link: '/dlna' },
      { text: '网页控制（中文 Beta）', link: '/website' },
      { text: '注意事项', link: '/notes' }
    ]
  },
  {
    text: '延伸阅读',
    items: [
      { text: '搭建媒体库', link: '/library' },
      { text: '设备怎么选', link: '/devices' }
    ]
  }
]

const englishSidebar = [
  {
    text: 'Getting started',
    items: [
      { text: 'Overview', link: '/en/' },
      { text: 'Connect a media server', link: '/en/media-server' },
      { text: 'Play files directly', link: '/en/files' },
      { text: 'IPTV', link: '/en/iptv' },
      { text: 'Receive DLNA casts', link: '/en/dlna' },
      { text: 'Web control (Chinese beta)', link: '/en/website' },
      { text: 'Notes and safety', link: '/en/notes' }
    ]
  },
  {
    text: 'Further reading',
    items: [
      { text: 'Build a media library', link: '/en/library' },
      { text: 'Choosing a device', link: '/en/devices' }
    ]
  }
]

export default defineConfig({
  title: 'TinyPlay',
  description: '让闲置 PC 接上电视，用手机控制 Jellyfin、NAS 文件、IPTV 和支持的网站播放。',
  lang: 'zh-CN',
  base: '/tinyplay/guide/',
  locales: {
    root: { label: '简体中文', lang: 'zh-CN' },
    en: {
      label: 'English',
      lang: 'en',
      title: 'TinyPlay',
      description: 'Use your phone to control Jellyfin, NAS files, IPTV, and supported websites on a PC connected to your TV.',
      themeConfig: {
        nav: [
          { text: 'TinyPlay home', link: 'https://yanghanqing.github.io/tinyplay/' },
          { text: 'Download', link: 'https://github.com/YangHanqing/tinyplay/releases/latest' }
        ],
        sidebar: englishSidebar,
        outline: { label: 'On this page', level: [2, 3] },
        docFooter: { prev: 'Previous', next: 'Next' }
      }
    }
  },
  outDir: '../site/guide',
  appearance: false,
  head: [
    ['meta', { name: 'theme-color', content: '#ffffff' }]
  ],
  themeConfig: {
    nav: [
      { text: 'TinyPlay 首页', link: 'https://yanghanqing.github.io/tinyplay/' },
      { text: '下载', link: 'https://github.com/YangHanqing/tinyplay/releases/latest' }
    ],
    sidebar: chineseSidebar,
    outline: { label: '本页内容', level: [2, 3] },
    docFooter: { prev: '上一页', next: '下一页' },
    i18nRouting: true,
    footer: {
      message: 'GPL-3.0 License',
      copyright: 'TinyPlay'
    }
  }
})
