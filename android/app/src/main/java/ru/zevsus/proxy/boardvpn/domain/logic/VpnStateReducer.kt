package ru.zevsus.proxy.boardvpn.domain.logic

import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingPolicy
import ru.zevsus.proxy.boardvpn.domain.model.VpnFailure
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState

sealed interface VpnEvent {
    val sessionId: VpnSessionId

    data class ConnectRequested(
        override val sessionId: VpnSessionId,
        val profileId: VpnProfileId,
    ) : VpnEvent

    data class TunnelRequested(override val sessionId: VpnSessionId) : VpnEvent
    data class TunnelEstablished(
        override val sessionId: VpnSessionId,
        val appRoutingPolicy: AppRoutingPolicy,
    ) : VpnEvent
    data class CoreConnected(override val sessionId: VpnSessionId) : VpnEvent
    data class TunStarted(
        override val sessionId: VpnSessionId,
        val elapsedRealtimeMillis: Long,
    ) : VpnEvent

    data class CoreReconnectStarted(
        override val sessionId: VpnSessionId,
        val attempt: Int,
        val reason: VpnFailure?,
    ) : VpnEvent

    data class DisconnectRequested(override val sessionId: VpnSessionId) : VpnEvent

    data class RuntimeFailed(
        override val sessionId: VpnSessionId,
        val failure: VpnFailure,
    ) : VpnEvent

    data class ShutdownCompleted(override val sessionId: VpnSessionId) : VpnEvent
}

object VpnStateReducer {
    fun reduce(state: VpnSessionState, event: VpnEvent): VpnSessionState = when (state) {
        VpnSessionState.Idle -> reduceIdle(event)
        is VpnSessionState.Active -> reduceActive(state, event)
        is VpnSessionState.Failed -> reduceFailed(state, event)
    }

    private fun reduceIdle(event: VpnEvent): VpnSessionState = when (event) {
        is VpnEvent.ConnectRequested -> event.startingState()
        else -> VpnSessionState.Idle
    }

    private fun reduceActive(
        state: VpnSessionState.Active,
        event: VpnEvent,
    ): VpnSessionState {
        if (event.sessionId != state.sessionId) return state

        return when (event) {
            is VpnEvent.ConnectRequested -> state
            is VpnEvent.DisconnectRequested -> state.withPhase(VpnSessionPhase.Stopping)
            is VpnEvent.RuntimeFailed -> VpnSessionState.Failed(
                sessionId = state.sessionId,
                profileId = state.profileId,
                failure = event.failure,
            )
            is VpnEvent.ShutdownCompleted -> when (state.phase) {
                VpnSessionPhase.Stopping -> VpnSessionState.Idle
                else -> state
            }
            is VpnEvent.TunnelRequested -> state.transition(
                from = VpnSessionPhase.Starting,
                to = VpnSessionPhase.RequestingTunnel,
            )
            is VpnEvent.TunnelEstablished -> {
                if (state.phase == VpnSessionPhase.RequestingTunnel) {
                    state.copy(
                        phase = VpnSessionPhase.ConnectingCore,
                        appliedAppRoutingPolicy = event.appRoutingPolicy,
                    )
                } else {
                    state
                }
            }
            is VpnEvent.CoreConnected -> when (state.phase) {
                VpnSessionPhase.ConnectingCore -> state.withPhase(VpnSessionPhase.StartingTun)
                is VpnSessionPhase.Reconnecting -> state.withPhase(VpnSessionPhase.Connected)
                else -> state
            }
            is VpnEvent.TunStarted -> {
                if (state.phase == VpnSessionPhase.StartingTun) {
                    state.copy(
                        phase = VpnSessionPhase.Connected,
                        connectedAtElapsedRealtimeMillis = event.elapsedRealtimeMillis,
                    )
                } else {
                    state
                }
            }
            is VpnEvent.CoreReconnectStarted -> when (state.phase) {
                VpnSessionPhase.Connected,
                is VpnSessionPhase.Reconnecting,
                -> state.withPhase(
                    VpnSessionPhase.Reconnecting(
                        attempt = event.attempt.coerceAtLeast(1),
                        reason = event.reason,
                    )
                )
                else -> state
            }
        }
    }

    private fun reduceFailed(
        state: VpnSessionState.Failed,
        event: VpnEvent,
    ): VpnSessionState = when (event) {
        is VpnEvent.ConnectRequested -> {
            if (event.sessionId != state.sessionId) event.startingState() else state
        }
        else -> state
    }

    private fun VpnEvent.ConnectRequested.startingState() = VpnSessionState.Active(
        sessionId = sessionId,
        profileId = profileId,
        phase = VpnSessionPhase.Starting,
    )

    private fun VpnSessionState.Active.withPhase(phase: VpnSessionPhase) = copy(phase = phase)

    private fun VpnSessionState.Active.transition(
        from: VpnSessionPhase,
        to: VpnSessionPhase,
    ): VpnSessionState = if (phase == from) withPhase(to) else this
}
