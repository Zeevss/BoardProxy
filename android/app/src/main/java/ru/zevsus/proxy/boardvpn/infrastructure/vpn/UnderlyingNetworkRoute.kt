package ru.zevsus.proxy.boardvpn.infrastructure.vpn

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.util.Log
import java.io.Closeable
import java.net.Inet4Address

/**
 * Keeps the core control-plane sockets and their DNS server on one physical network.
 *
 * A protected socket is outside the VPN, but it is not automatically pinned to the
 * same network from which an arbitrary ConnectivityManager callback obtained DNS.
 */
class UnderlyingNetworkRoute(
    context: Context,
    private val onNetworkChanged: (String) -> Unit = {},
) : Closeable {
    private val connectivity = context.getSystemService(ConnectivityManager::class.java)
    private val lock = Any()
    private val propertiesByNetwork = linkedMapOf<Network, LinkProperties>()

    @Volatile
    private var currentNetwork: Network? = connectivity.activeNetwork
        ?.takeIf(::isUnderlyingNetwork)

    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) {
            val changed = synchronized(lock) {
                if (currentNetwork == network) {
                    false
                } else {
                    currentNetwork = network
                    true
                }
            }
            logSelection("available")
            if (changed) {
                onNetworkChanged("physical network available")
            }
        }

        override fun onLinkPropertiesChanged(network: Network, properties: LinkProperties) {
            val dnsChanged = synchronized(lock) {
                val previousDns = propertiesByNetwork[network]?.preferredDnsAddress()
                propertiesByNetwork[network] = properties
                network == currentNetwork &&
                    previousDns != null &&
                    previousDns != properties.preferredDnsAddress()
            }
            if (dnsChanged) {
                logSelection("dns changed")
                onNetworkChanged("underlying DNS changed")
            }
        }

        override fun onLost(network: Network) {
            val changed = synchronized(lock) {
                propertiesByNetwork.remove(network)
                if (currentNetwork == network) {
                    currentNetwork = propertiesByNetwork.keys.lastOrNull()
                    true
                } else {
                    false
                }
            }
            if (changed) {
                logSelection("lost")
                onNetworkChanged("physical network lost")
            }
        }
    }

    init {
        currentNetwork?.let { network ->
            connectivity.getLinkProperties(network)?.let { properties ->
                propertiesByNetwork[network] = properties
            }
        }
        logSelection("initial")

        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
            .build()

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            // Unlike registerNetworkCallback, this callback follows the single
            // best network matching the request when Wi-Fi/mobile preference changes.
            connectivity.registerBestMatchingNetworkCallback(
                request,
                callback,
                Handler(Looper.getMainLooper()),
            )
        } else {
            connectivity.registerNetworkCallback(request, callback)
        }
    }

    fun protectAndBind(service: VpnService, fileDescriptor: Int): Boolean {
        if (!service.protect(fileDescriptor)) return false

        // Android 8-11 has no callback for the single best matching network.
        // Let a protected socket follow the system default route instead of
        // pinning it to a possibly stale secondary network.
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) return true

        val network = currentNetwork ?: return true
        runCatching {
            // fromFd duplicates the descriptor. Binding the duplicate changes the
            // underlying socket while allowing Go to retain ownership of the original.
            ParcelFileDescriptor.fromFd(fileDescriptor).use { duplicate ->
                network.bindSocket(duplicate.fileDescriptor)
            }
        }.onFailure { error ->
            // Some Android builds (notably Samsung One UI) reject binding an
            // already protected descriptor with EPERM. Protection is sufficient
            // to keep the socket outside the VPN, so network pinning is optional.
            Log.w(TAG, "network pinning unavailable fd=$fileDescriptor network=$network", error)
        }
        return true
    }

    fun dnsAddress(): String = if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) {
        // A per-network private DNS address is unsafe when the socket itself
        // follows the default route and that route can change underneath it.
        FALLBACK_DNS_ADDRESS
    } else synchronized(lock) {
        currentNetwork
            ?.let(propertiesByNetwork::get)
            ?.preferredDnsAddress()
            ?: FALLBACK_DNS_ADDRESS
    }

    override fun close() {
        runCatching { connectivity.unregisterNetworkCallback(callback) }
    }

    private fun isUnderlyingNetwork(network: Network): Boolean = connectivity
        .getNetworkCapabilities(network)
        ?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
        ?: false

    private fun logSelection(reason: String) {
        val (network, dns) = synchronized(lock) {
            val network = currentNetwork
            network to (
                network?.let(propertiesByNetwork::get)?.preferredDnsAddress()
                    ?: FALLBACK_DNS_ADDRESS
                )
        }
        Log.i(TAG, "underlying network reason=$reason network=$network dns=$dns")
    }

    private companion object {
        const val TAG = "BoardVpnNetwork"
    }
}

private fun LinkProperties.preferredDnsAddress(): String? =
    dnsServers.firstOrNull { it is Inet4Address }?.hostAddress
        ?: dnsServers.firstOrNull()?.hostAddress

private const val FALLBACK_DNS_ADDRESS = "1.1.1.1"
