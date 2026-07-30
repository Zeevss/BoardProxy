package ru.zevsus.proxy.boardvpn.domain.logic

import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Test
import ru.zevsus.proxy.boardvpn.domain.model.VpnFailure
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState

class VpnStateReducerTest {
    private val sessionId = VpnSessionId(1)
    private val profileId = VpnProfileId("profile")

    @Test
    fun `connect follows the complete startup path`() {
        var state: VpnSessionState = VpnSessionState.Idle

        state = reduce(state, VpnEvent.ConnectRequested(sessionId, profileId))
        assertPhase<VpnSessionPhase.Starting>(state)
        state = reduce(state, VpnEvent.TunnelRequested(sessionId))
        assertPhase<VpnSessionPhase.RequestingTunnel>(state)
        state = reduce(state, VpnEvent.TunnelEstablished(sessionId))
        assertPhase<VpnSessionPhase.ConnectingCore>(state)
        state = reduce(state, VpnEvent.CoreConnected(sessionId))
        assertPhase<VpnSessionPhase.StartingTun>(state)
        state = reduce(state, VpnEvent.TunStarted(sessionId))
        assertPhase<VpnSessionPhase.Connected>(state)
    }

    @Test
    fun `core reconnect keeps the same VPN session`() {
        val reconnecting = reduce(
            active(VpnSessionPhase.Connected),
            VpnEvent.CoreReconnectStarted(
                sessionId,
                attempt = 2,
                reason = VpnFailure.CoreConnectionLost("timeout"),
            ),
        )
        assertPhase<VpnSessionPhase.Reconnecting>(reconnecting)

        assertEquals(
            active(VpnSessionPhase.Connected),
            reduce(reconnecting, VpnEvent.CoreConnected(sessionId)),
        )
    }

    @Test
    fun `disconnect reaches idle only after shutdown completes`() {
        val stopping = reduce(
            active(VpnSessionPhase.Connected),
            VpnEvent.DisconnectRequested(sessionId),
        )
        assertPhase<VpnSessionPhase.Stopping>(stopping)

        assertSame(
            VpnSessionState.Idle,
            reduce(stopping, VpnEvent.ShutdownCompleted(sessionId)),
        )
    }

    @Test
    fun `stale and invalid events are ignored`() {
        val state = active(VpnSessionPhase.Starting)

        assertSame(state, reduce(state, VpnEvent.TunStarted(sessionId)))
        assertSame(
            state,
            reduce(
                state,
                VpnEvent.RuntimeFailed(
                    VpnSessionId(999),
                    VpnFailure.Unexpected("stale"),
                ),
            ),
        )
    }

    private fun active(phase: VpnSessionPhase) = VpnSessionState.Active(
        sessionId = sessionId,
        profileId = profileId,
        phase = phase,
    )

    private fun reduce(state: VpnSessionState, event: VpnEvent) =
        VpnStateReducer.reduce(state, event)

    private inline fun <reified T : VpnSessionPhase> assertPhase(state: VpnSessionState) {
        val active = state as VpnSessionState.Active
        check(active.phase is T) {
            "Expected ${T::class.simpleName}, got ${active.phase}"
        }
    }
}
