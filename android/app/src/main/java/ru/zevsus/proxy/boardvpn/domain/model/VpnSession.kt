package ru.zevsus.proxy.boardvpn.domain.model

@JvmInline
value class VpnSessionId(val value: Long)

sealed interface VpnSessionPhase {
    data object Starting : VpnSessionPhase
    data object RequestingTunnel : VpnSessionPhase
    data object ConnectingCore : VpnSessionPhase
    data object StartingTun : VpnSessionPhase
    data object Connected : VpnSessionPhase

    data class Reconnecting(
        val attempt: Int,
        val reason: VpnFailure?,
    ) : VpnSessionPhase

    data object Stopping : VpnSessionPhase
}

sealed interface VpnSessionState {
    data object Idle : VpnSessionState

    data class Active(
        val sessionId: VpnSessionId,
        val profileId: VpnProfileId,
        val phase: VpnSessionPhase,
    ) : VpnSessionState

    data class Failed(
        val sessionId: VpnSessionId?,
        val profileId: VpnProfileId?,
        val failure: VpnFailure,
    ) : VpnSessionState
}
