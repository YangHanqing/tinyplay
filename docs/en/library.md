# Build a media library

If you want posters, episode browsing, search, and resume playback, set up a media server first and connect TinyPlay to it. The server organises the content; TinyPlay lets you browse it from your phone and play it on the computer connected to the TV.

## Recommended: Jellyfin

Jellyfin is a straightforward option: it is free and open source, supports Docker, and has a large community. Many NAS systems also provide a convenient installation package.

Once it is running, choose **Jellyfin** under **Add media source** in TinyPlay and enter the server address and account details.

## Organise the folders first

The media server needs to know where your movies and shows are stored. Keep movies, TV shows, and animation in separate folders when possible. For shows, use a structure such as “Title / Season / episode file” so the server can match posters, metadata, and subtitles reliably.

```text
Media/
├── Movies/
│   └── Movie Name (Year).mkv
└── TV Shows/
    └── Show Name/
        └── Season 01/
            └── S01E01.mkv
```

## You do not need to configure everything at once

Let the server identify one folder correctly, then connect TinyPlay and play one video. You can fill in posters, metadata, remote access, and other details later as needed.
