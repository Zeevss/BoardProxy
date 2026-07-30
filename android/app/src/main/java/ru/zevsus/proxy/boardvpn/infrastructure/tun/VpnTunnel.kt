package ru.zevsus.proxy.boardvpn.infrastructure.tun

import android.os.ParcelFileDescriptor
import android.net.VpnService
import java.io.Closeable

data class VpnTunnelConfig(
    val sessionName: String = "BoardVPN",
    val address: String = "10.89.0.2",
    val addressPrefixLength: Int = 32,
    val mtu: Int = 1_500,
    val routes: List<IpNetwork> = listOf(IpNetwork("0.0.0.0", 0)),
    // hev mapdns answers this address locally; TCP and application UDP are
    // carried through the local BoardProxy SOCKS5 endpoint.
    val dnsServers: List<String> = listOf("1.1.1.1"),
)

data class IpNetwork(
    val address: String,
    val prefixLength: Int,
)

class VpnTunnel internal constructor(
    private val descriptor: ParcelFileDescriptor,
) : Closeable {
    val fileDescriptor: Int
        get() = descriptor.fd

    override fun close() {
        descriptor.close()
    }
}

class VpnTunnelFactory(
    private val service: VpnService,
    private val config: VpnTunnelConfig = VpnTunnelConfig(),
) {
    fun establish(): VpnTunnel {
        val builder = service.Builder()
            .setSession(config.sessionName)
            .setMtu(config.mtu)
            .setBlocking(true)
            .addAddress(config.address, config.addressPrefixLength)

        config.routes.forEach { builder.addRoute(it.address, it.prefixLength) }
        config.dnsServers.forEach(builder::addDnsServer)

        val descriptor = checkNotNull(builder.establish()) {
            "VpnService.Builder.establish() returned null"
        }
        return VpnTunnel(descriptor)
    }
}
