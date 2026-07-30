package ru.zevsus.proxy.boardvpn.infrastructure.tun

import android.content.Context
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.delay

/** TCP TUN-to-SOCKS forwarding backed by hev-socks5-tunnel. */
class HevPacketForwarder(
    context: Context,
) : PacketForwarder {
    private val configFile = File(context.cacheDir, CONFIG_FILE_NAME)
    private val started = AtomicBoolean()

    override suspend fun start(tunFileDescriptor: Int, socksAddress: String) {
        check(started.compareAndSet(false, true)) { "Packet forwarder is already running" }

        val endpoint = SocksEndpoint.parse(socksAddress)
        try {
            configFile.writeText(buildConfig(endpoint))
            check(
                HevSocks5TunnelBridge.TProxyStartService(
                    configFile.absolutePath,
                    tunFileDescriptor,
                )
            ) { "hev-socks5-tunnel rejected the start request" }

            // Native start creates its worker thread asynchronously. Catch immediate
            // configuration and descriptor failures before publishing Connected.
            delay(STARTUP_PROBE_MILLIS)
            check(HevSocks5TunnelBridge.TProxyIsRunning()) {
                "hev-socks5-tunnel stopped during startup"
            }
        } catch (error: Throwable) {
            started.set(false)
            HevSocks5TunnelBridge.TProxyStopService()
            configFile.delete()
            throw error
        }
    }

    override suspend fun awaitTermination() {
        while (started.get() && HevSocks5TunnelBridge.TProxyIsRunning()) {
            delay(TERMINATION_POLL_MILLIS)
        }
    }

    override suspend fun stop() {
        stopNative()
    }

    override fun closeImmediately() {
        runCatching { stopNative() }
    }

    private fun stopNative() {
        if (!started.compareAndSet(true, false)) return
        check(HevSocks5TunnelBridge.TProxyStopService()) {
            "hev-socks5-tunnel failed to stop"
        }
        configFile.delete()
    }

    private fun buildConfig(endpoint: SocksEndpoint): String = """
        tunnel:
          mtu: 1500
          ipv4: '10.89.0.2'
          icmp: 'reply'
        socks5:
          address: '${endpoint.host}'
          port: ${endpoint.port}
          udp: 'udp'
        mapdns:
          address: 1.1.1.1
          port: 53
          network: 240.0.0.0
          netmask: 240.0.0.0
          cache-size: 4096
        misc:
          log-file: stderr
          log-level: warn
    """.trimIndent()

    private data class SocksEndpoint(
        val host: String,
        val port: Int,
    ) {
        companion object {
            fun parse(value: String): SocksEndpoint {
                val separator = value.lastIndexOf(':')
                require(separator > 0 && separator < value.lastIndex) {
                    "Invalid SOCKS endpoint"
                }
                val host = value.substring(0, separator).removeSurrounding("[", "]")
                require(host.matches(Regex("[A-Za-z0-9.:-]+"))) {
                    "Invalid SOCKS host"
                }
                val port = value.substring(separator + 1).toIntOrNull()
                require(port != null && port in 1..65535) { "Invalid SOCKS port" }
                return SocksEndpoint(host, port)
            }
        }
    }

    private companion object {
        const val CONFIG_FILE_NAME = "hev-socks5-tunnel.yml"
        const val STARTUP_PROBE_MILLIS = 100L
        const val TERMINATION_POLL_MILLIS = 250L
    }
}
