package ru.zevsus.proxy.boardvpn.infrastructure.tun;

/** JNI surface registered by libhev-socks5-tunnel.so. */
public final class HevSocks5TunnelBridge {
    static {
        System.loadLibrary("hev-socks5-tunnel");
    }

    private HevSocks5TunnelBridge() {}

    public static native boolean TProxyStartService(String configPath, int tunFileDescriptor);

    public static native boolean TProxyStopService();

    public static native boolean TProxyIsRunning();

    public static native long[] TProxyGetStats();
}
