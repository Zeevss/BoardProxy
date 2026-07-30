package ru.zevsus.proxy.boardvpn.infrastructure.core

import io.boardproxy.mobile.Client
import io.boardproxy.mobile.Listener
import io.boardproxy.mobile.Mobile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import ru.zevsus.proxy.boardvpn.domain.model.ProxyStreamStatistics
import ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics

class AarBoardProxyClientFactory : BoardProxyClientFactory {
    override fun create(
        config: BoardProxyConfig,
        listener: BoardProxyListener,
        socketProtector: SocketProtector,
    ): BoardProxyClient {
        val configJson = JSONObject()
            .put("keylink", config.keylink.reveal())
            .put("listen", config.listenAddress)
            .put("log_level", config.logLevel)
            .put("enable_udp", config.enableUdp)
            .toString()

        val aarClient = Mobile.createClient(
            configJson,
            AarListener(listener),
            object : io.boardproxy.mobile.SocketProtector {
                override fun protect(fd: Long): Boolean =
                    socketProtector.protect(fd.toInt())

                override fun dnsAddress(): String = socketProtector.dnsAddress()
            },
        )
        return AarBoardProxyClient(aarClient)
    }
}

private class AarBoardProxyClient(
    private val client: Client,
) : BoardProxyClient {
    override fun start() {
        client.start()
    }

    override fun stop() {
        client.stop()
    }

    override fun reconnect(): Boolean = client.reconnect()

    override suspend fun awaitTermination() {
        withContext(Dispatchers.IO) { client.awaitTermination() }
    }
}

private class AarListener(
    private val listener: BoardProxyListener,
) : Listener {
    override fun onStatus(status: String, message: String) {
        listener.onStatus(status.toBoardProxyStatus(), message.ifBlank { null })
    }

    override fun onLog(level: String, message: String) {
        listener.onLog(level, message)
    }

    override fun onMetrics(metricsJSON: String) {
        runCatching { parseStatistics(metricsJSON) }
            .onSuccess(listener::onMetrics)
            .onFailure { listener.onLog("warn", "Invalid metrics payload: ${it.message}") }
    }
}

private fun String.toBoardProxyStatus(): BoardProxyStatus = when (this) {
    "disconnected" -> BoardProxyStatus.Disconnected
    "connecting" -> BoardProxyStatus.Connecting
    "connected" -> BoardProxyStatus.Connected
    "reconnecting" -> BoardProxyStatus.Reconnecting
    "stopping" -> BoardProxyStatus.Stopping
    "error" -> BoardProxyStatus.Error
    else -> BoardProxyStatus.Error
}

private fun parseStatistics(json: String): VpnStatistics {
    val root = JSONObject(json)
    val details = root.optJSONArray("details")
    val streams = buildList {
        if (details != null) {
            for (index in 0 until details.length()) {
                val stream = details.getJSONObject(index)
                add(
                    ProxyStreamStatistics(
                        id = stream.optLong("id"),
                        target = stream.optString("target"),
                        uploadedBytes = stream.optLong("tx"),
                        downloadedBytes = stream.optLong("rx"),
                        startedAtEpochMillis = stream.optLong("started_ms"),
                    )
                )
            }
        }
    }

    return VpnStatistics(
        roundTripTimeMillis = root.optLong("rtt_ms").takeIf { it > 0 },
        activeStreams = root.optInt("streams"),
        uploadedBytes = root.optLong("total_tx"),
        downloadedBytes = root.optLong("total_rx"),
        uploadBytesPerSecond = root.optLong("rate_tx"),
        downloadBytesPerSecond = root.optLong("rate_rx"),
        streams = streams,
    )
}
