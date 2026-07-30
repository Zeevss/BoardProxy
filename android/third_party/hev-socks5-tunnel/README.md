# hev-socks5-tunnel

BoardVPN embeds `hev-socks5-tunnel` commit
`d1b6c6f1a02e2010dbed795d8a40fdc4155e49b2` under the MIT license. This is the
upstream Android lifecycle revision immediately after release 2.16.0; it adds
the boolean start/stop and running-state JNI API used by BoardVPN.

The checked-in `libhev-socks5-tunnel.so` files are built for `arm64-v8a`,
`armeabi-v7a` and `x86_64`. JNI is registered against
`ru.zevsus.proxy.boardvpn.infrastructure.tun.HevSocks5TunnelBridge`, and ELF
segments are aligned for 16 KiB Android page sizes.

Rebuild them with `scripts/build-hev-socks5-tunnel.sh /path/to/android-ndk`.

Upstream: https://github.com/heiher/hev-socks5-tunnel
