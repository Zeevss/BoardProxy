package ru.zevsus.proxy.boardvpn.domain.model

sealed interface VpnFailure {
    val technicalMessage: String?

    data class InvalidProfile(override val technicalMessage: String? = null) : VpnFailure
    data class PermissionRevoked(override val technicalMessage: String? = null) : VpnFailure
    data class TunnelEstablishmentFailed(override val technicalMessage: String? = null) : VpnFailure
    data class CoreStartFailed(override val technicalMessage: String? = null) : VpnFailure
    data class CoreConnectionLost(override val technicalMessage: String? = null) : VpnFailure
    data class TunEngineFailed(override val technicalMessage: String? = null) : VpnFailure
    data class SocketProtectionFailed(override val technicalMessage: String? = null) : VpnFailure
    data class ShutdownTimedOut(override val technicalMessage: String? = null) : VpnFailure
    data class Unexpected(override val technicalMessage: String? = null) : VpnFailure
}
