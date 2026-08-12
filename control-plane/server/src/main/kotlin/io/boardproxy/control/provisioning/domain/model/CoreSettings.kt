package io.boardproxy.control.provisioning.domain.model

import java.net.InetAddress
import java.time.Duration

data class ServerSettings(
    val privateKey: String,
    val idleTimeout: Duration,
    val allowPrivateEgress: Boolean = false,
)

data class TransportSettings(
    val window: Int = 0,
    val maxFramePayload: Int = 4 shl 20,
    val streamWindow: Int = 1 shl 20,
    val maxStreamWindow: Int = 32 shl 20,
    val ackTimeout: Duration = Duration.ofSeconds(6),
    val coalesceTarget: Int = 0,
    val streamIdleTimeout: Duration = Duration.ZERO,
)

data class ManagementSettings(
    val grpcListen: String = "unix:///run/bproxy/control.sock",
    val httpListen: String? = null,
)

data class ObservabilitySettings(
    val enabled: Boolean = true,
    val logLevel: String = "info",
)

data class CoreSettings(
    val server: ServerSettings,
    val transport: TransportSettings = TransportSettings(),
    val management: ManagementSettings = ManagementSettings(),
    val observability: ObservabilitySettings = ObservabilitySettings(),
) {
    internal fun validate() {
        KeyMaterial.decodePrivate(server.privateKey)
        requireDomain(!server.idleTimeout.isNegative, "negative idle timeout")
        requireDomain(
            transport.window >= 0 && transport.coalesceTarget >= 0 && transport.maxFramePayload > 0 &&
                transport.streamWindow > 0 && transport.maxStreamWindow >= transport.streamWindow &&
                !transport.ackTimeout.isNegative && !transport.ackTimeout.isZero &&
                !transport.streamIdleTimeout.isNegative,
            "invalid transport settings",
        )
        validateListener(management.grpcListen, allowUnix = true)
        management.httpListen?.takeIf(String::isNotBlank)?.let { validateListener(it, allowUnix = false) }
        requireDomain(observability.logLevel in setOf("debug", "info", "warn", "error"), "invalid log level")
    }

    companion object {
        fun defaults(serverPrivateKey: String) = CoreSettings(
            server = ServerSettings(serverPrivateKey, Duration.ofSeconds(90)),
        )
    }
}

private fun validateListener(value: String, allowUnix: Boolean) {
    if (allowUnix && value.startsWith("unix://")) {
        requireDomain(value.removePrefix("unix://").isNotBlank(), "empty Unix listener path")
        return
    }
    val address = value.removePrefix("tcp://")
    val separator = address.lastIndexOf(':')
    requireDomain(separator > 0 && separator < address.lastIndex, "listener must be host:port")
    val host = address.substring(0, separator).removeSurrounding("[", "]")
    val port = address.substring(separator + 1).toIntOrNull()
    val loopback = host == "localhost" || runCatching { InetAddress.getByName(host).isLoopbackAddress }.getOrDefault(false)
    requireDomain(loopback && port != null && port in 1..65535, "plaintext listener must use loopback")
}
