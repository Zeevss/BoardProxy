package ru.zevsus.proxy.boardvpn.infrastructure.core

import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics

data class BoardProxyConfig(
    val keylink: BoardProxyKeylink,
    val listenAddress: String = "127.0.0.1:1080",
    val logLevel: String = "info",
    val enableUdp: Boolean = false,
)

enum class BoardProxyStatus {
    Disconnected,
    Connecting,
    Connected,
    Reconnecting,
    Stopping,
    Error,
}

interface BoardProxyListener {
    fun onStatus(status: BoardProxyStatus, message: String?)
    fun onMetrics(statistics: VpnStatistics)
    fun onLog(level: String, message: String)
}

interface SocketProtector {
    fun protect(fileDescriptor: Int): Boolean

    fun dnsAddress(): String
}

interface BoardProxyClient {
    fun start()
    fun stop()
    fun reconnect(): Boolean
    suspend fun awaitTermination()
}

fun interface BoardProxyClientFactory {
    fun create(
        config: BoardProxyConfig,
        listener: BoardProxyListener,
        socketProtector: SocketProtector,
    ): BoardProxyClient
}
