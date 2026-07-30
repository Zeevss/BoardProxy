package ru.zevsus.proxy.boardvpn.infrastructure.fake

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.VpnConnectResult
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState

class FakeVpnRepositoryTest {
    private val profileId = VpnProfileId("home")
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    @Test
    fun `fake repository connects and disconnects`() = runBlocking {
        val profiles = InMemoryVpnProfileRepository(
            listOf(
                VpnProfile(
                    id = profileId,
                    name = "Home",
                    keylink = BoardProxyKeylink.fromRaw("bproxy://${"A".repeat(86)}"),
                )
            )
        )
        val repository = FakeVpnRepository(
            profiles = profiles,
            scope = scope,
            timing = FakeVpnTiming(
                startupStepMillis = 1,
                reconnectMillis = 1,
                statisticsTickMillis = 10,
            ),
        )

        try {
            assertEquals(VpnConnectResult.Started, repository.connect(profileId))

            val connected = withTimeout(1_000) {
                repository.observeSession().filter { state ->
                    state is VpnSessionState.Active &&
                        state.phase == VpnSessionPhase.Connected
                }.first()
            }
            assertTrue(connected is VpnSessionState.Active)

            repository.disconnect()
            assertSameState(VpnSessionState.Idle, repository.observeSession().first())
        } finally {
            scope.cancel()
        }
    }

    @Test
    fun `missing profile is rejected`() = runBlocking {
        val repository = FakeVpnRepository(
            profiles = InMemoryVpnProfileRepository(),
            scope = scope,
        )

        try {
            assertEquals(
                VpnConnectResult.ProfileNotFound(profileId),
                repository.connect(profileId),
            )
        } finally {
            scope.cancel()
        }
    }

    private fun assertSameState(expected: VpnSessionState, actual: VpnSessionState) {
        assertEquals(expected, actual)
    }
}
