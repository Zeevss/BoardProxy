package ru.zevsus.proxy.boardvpn.infrastructure.vpn.tile

import android.service.quicksettings.Tile
import org.junit.Assert.assertEquals
import org.junit.Test
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState

class BoardVpnTileStateTest {
    @Test
    fun idleIsInactive() {
        assertEquals(
            TilePresentation(Tile.STATE_INACTIVE, R.string.home_status_disconnected),
            VpnSessionState.Idle.toTilePresentation(),
        )
    }

    @Test
    fun connectedIsActive() {
        assertEquals(
            TilePresentation(Tile.STATE_ACTIVE, R.string.home_status_connected),
            active(VpnSessionPhase.Connected).toTilePresentation(),
        )
    }

    @Test
    fun connectionTransitionsAreUnavailable() {
        listOf(
            VpnSessionPhase.Starting,
            VpnSessionPhase.RequestingTunnel,
            VpnSessionPhase.ConnectingCore,
            VpnSessionPhase.StartingTun,
            VpnSessionPhase.Reconnecting(attempt = 1, reason = null),
            VpnSessionPhase.Stopping,
        ).forEach { phase ->
            assertEquals(Tile.STATE_UNAVAILABLE, active(phase).toTilePresentation().state)
        }
    }

    private fun active(phase: VpnSessionPhase) = VpnSessionState.Active(
        sessionId = VpnSessionId(1),
        profileId = VpnProfileId("profile"),
        phase = phase,
    )
}
