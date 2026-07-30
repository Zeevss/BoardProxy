#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/android-ndk" >&2
  exit 2
fi

ndk_dir=$1
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
android_dir=$(cd -- "$script_dir/.." && pwd)
source_dir=$(mktemp -d)
trap 'rm -rf -- "$source_dir"' EXIT
upstream_commit=d1b6c6f1a02e2010dbed795d8a40fdc4155e49b2

git init "$source_dir/hev"
git -C "$source_dir/hev" remote add origin \
  https://github.com/heiher/hev-socks5-tunnel.git
git -C "$source_dir/hev" fetch --depth 1 origin "$upstream_commit"
git -C "$source_dir/hev" checkout --detach FETCH_HEAD
git -C "$source_dir/hev" submodule update --init --recursive --depth 1

"$ndk_dir/ndk-build" \
  NDK_PROJECT_PATH="$source_dir/hev" \
  APP_BUILD_SCRIPT="$source_dir/hev/Android.mk" \
  NDK_APPLICATION_MK="$source_dir/hev/Application.mk" \
  APP_ABI='armeabi-v7a arm64-v8a x86_64' \
  APP_PLATFORM=android-26 \
  'APP_CFLAGS+=-DPKGNAME=ru/zevsus/proxy/boardvpn/infrastructure/tun -DCLSNAME=HevSocks5TunnelBridge'

for abi in armeabi-v7a arm64-v8a x86_64; do
  install -D -m 0644 \
    "$source_dir/hev/libs/$abi/libhev-socks5-tunnel.so" \
    "$android_dir/app/src/main/jniLibs/$abi/libhev-socks5-tunnel.so"
done
