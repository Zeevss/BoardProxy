package ru.zevsus.proxy.boardvpn.infrastructure.tun

import android.content.pm.PackageManager
import android.net.VpnService
import android.os.ParcelFileDescriptor
import java.io.Closeable
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingMode
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingPolicy

data class VpnTunnelConfig(
    val sessionName: String = "BoardVPN",
    val address: String = "10.89.0.2",
    val addressPrefixLength: Int = 32,
    val mtu: Int = 1_500,
    val routes: List<IpNetwork> = listOf(IpNetwork("0.0.0.0", 0)),
    // hev mapdns answers this address locally; TCP and application UDP are
    // carried through the local BoardProxy SOCKS5 endpoint.
    val dnsServers: List<String> = listOf("1.1.1.1"),
    val appRoutingPolicy: AppRoutingPolicy = AppRoutingPolicy.AllApps,
)

data class IpNetwork(
    val address: String,
    val prefixLength: Int,
)

class VpnTunnel internal constructor(
    private val descriptor: ParcelFileDescriptor,
    val appRoutingPolicy: AppRoutingPolicy,
) : Closeable {
    val fileDescriptor: Int
        get() = descriptor.fd

    override fun close() {
        descriptor.close()
    }
}

class VpnTunnelFactory(
    private val service: VpnService,
    private val configProvider: suspend () -> VpnTunnelConfig = { VpnTunnelConfig() },
) {
    suspend fun establish(): VpnTunnel {
        val config = configProvider()
        val builder = service.Builder()
            .setSession(config.sessionName)
            .setMtu(config.mtu)
            .setBlocking(true)
            .addAddress(config.address, config.addressPrefixLength)

        config.routes.forEach { builder.addRoute(it.address, it.prefixLength) }
        config.dnsServers.forEach(builder::addDnsServer)
        builder.apply(config.appRoutingPolicy)

        val descriptor = checkNotNull(builder.establish()) {
            "VpnService.Builder.establish() returned null"
        }
        return VpnTunnel(descriptor, config.appRoutingPolicy)
    }

    private fun VpnService.Builder.apply(policy: AppRoutingPolicy) {
        if (policy.allProxy) return

        val packages = policy.packageNames.sorted()
        try {
            when (policy.mode) {
                AppRoutingMode.AllApps -> Unit
                AppRoutingMode.OnlySelectedApps -> packages.forEach(::addAllowedApplication)
                AppRoutingMode.ExcludeSelectedApps -> packages.forEach(::addDisallowedApplication)
            }
        } catch (error: PackageManager.NameNotFoundException) {
            throw IllegalArgumentException(
                "Split-tunnel application is not installed: ${error.message}",
                error,
            )
        }
    }
}
