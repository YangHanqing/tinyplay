# Notes and safety

## Screensaver

TinyPlay's simple screensaver helps avoid leaving a TV on the same still frame for a long time.

After a video has been paused for more than five minutes, the screensaver appears. It darkens the current video's poster and shows the time, date, and title. The background image changes roughly once a minute.

Resume playback, press a remote-control button, or stop playback to dismiss the screensaver.

This is not a full system screensaver and is not designed for power saving or privacy. Its main purpose is to avoid leaving one static image on the TV.

## Device pairing

Every phone pairs with the computer once before it can control anything. Until it
does, TinyPlay answers nothing.

Scanning the QR code in the TinyPlay window is the normal way in — the code
carries a long random secret, so pairing completes with nothing to confirm. If
you type the computer's address into a browser instead, TinyPlay asks for
permission on the computer: your phone shows four digits, the TinyPlay window
asks whether to allow that same number, and you choose Allow.

If some device repeatedly submits a wrong pairing code, the QR code switches
itself off and the window explains why. **Show a new QR code** in that window
turns it back on with a fresh secret. Already-paired phones keep working
throughout — a stranger cannot lock you out of your own remote.

A paired phone stays paired. Add the remote to your Home Screen so the pairing
survives: otherwise iOS Safari clears website storage after about a week without
a visit, and the phone has to scan again. **Unpair all** in the TinyPlay window
revokes every phone at once and replaces the QR code, which is what to use if a
phone is lost or was lent out.

## Safety reminder

TinyPlay is designed for use on a trusted home LAN. Pairing stops other devices
on the same network from controlling the computer, but the connection is plain
HTTP and there is no account system, so it is not hardened for direct internet
access.

Do not expose TinyPlay's phone control page to the public internet through port forwarding, a reverse proxy, or a network tunnel. Keep your phone and the computer running TinyPlay on a network you trust.

This warning concerns the control page served by TinyPlay on your computer, not the TinyPlay product website hosted on GitHub Pages.

## Boundaries for web services

Web control operates websites already opened on the user's own Windows or macOS computer. Sign in yourself and follow each service's rules.

TinyPlay does not provide a VPN, proxy, regional-access bypass, ad or membership bypass, device-limit bypass, downloading, recording, rebroadcasting, or stream URL export. If a service changes its website, membership, or device rules, those rules apply.
