package ru.zevsus.proxy.boardvpn.ui.home

import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.infrastructure.fake.FakeVpnRepository
import ru.zevsus.proxy.boardvpn.infrastructure.fake.FakeVpnTiming
import ru.zevsus.proxy.boardvpn.infrastructure.fake.InMemoryVpnProfileRepository
import ru.zevsus.proxy.boardvpn.test.MainDispatcherRule

@OptIn(ExperimentalCoroutinesApi::class)
class HomeViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private val profile = VpnProfile(
        id = VpnProfileId("home"),
        name = "Home",
        keylink = BoardProxyKeylink.fromRaw("bproxy://${"A".repeat(86)}"),
    )

    @Test
    fun `repository lifecycle is reduced to compact UI statuses`() = runTest {
        val profiles = InMemoryVpnProfileRepository(listOf(profile))
        val viewModel = HomeViewModel(
            fakeVpn(profiles),
            profiles,
            elapsedRealtimeMillis = { testScheduler.currentTime },
        )
        collectState(viewModel)

        assertEquals(HomeConnectionStatus.Disconnected, viewModel.uiState.value.status)
        assertNotNull(viewModel.uiState.value.selectedProfile)
        assertNull(viewModel.uiState.value.connectedDuration)

        viewModel.onVpnPermissionDenied()
        runCurrent()
        assertEquals(HomeProblem.PermissionDenied, viewModel.uiState.value.problem)

        viewModel.connect()
        runCurrent()
        assertEquals(HomeConnectionStatus.Connecting, viewModel.uiState.value.status)

        advanceTimeBy(5)
        runCurrent()
        assertEquals(HomeConnectionStatus.Connected, viewModel.uiState.value.status)

        viewModel.onAction(HomeAction.ToggleConnection)
        runCurrent()
        assertEquals(HomeConnectionStatus.Disconnected, viewModel.uiState.value.status)
    }

    @Test
    fun `session timer runs while connected and resets afterwards`() = runTest {
        val profiles = InMemoryVpnProfileRepository(listOf(profile))
        val viewModel = HomeViewModel(
            fakeVpn(profiles),
            profiles,
            elapsedRealtimeMillis = { testScheduler.currentTime },
        )
        collectState(viewModel)

        viewModel.connect()
        advanceTimeBy(5)
        runCurrent()
        assertEquals(HomeConnectionStatus.Connected, viewModel.uiState.value.status)

        advanceTimeBy(3_100)
        runCurrent()
        assertEquals(3.seconds, viewModel.uiState.value.connectedDuration)

        viewModel.onAction(HomeAction.ToggleConnection)
        runCurrent()
        assertNull(viewModel.uiState.value.connectedDuration)
    }

    @Test
    fun `session timer survives view model recreation`() = runTest {
        val profiles = InMemoryVpnProfileRepository(listOf(profile))
        val vpn = fakeVpn(profiles)
        val first = HomeViewModel(
            vpn,
            profiles,
            elapsedRealtimeMillis = { testScheduler.currentTime },
        )
        collectState(first)

        first.connect()
        advanceTimeBy(5)
        runCurrent()
        advanceTimeBy(3_100)
        runCurrent()
        assertEquals(3.seconds, first.uiState.value.connectedDuration)

        val recreated = HomeViewModel(
            vpn,
            profiles,
            elapsedRealtimeMillis = { testScheduler.currentTime },
        )
        collectState(recreated)

        assertEquals(3.seconds, recreated.uiState.value.connectedDuration)
    }

    @Test
    fun `selecting a profile is stored in the repository`() = runTest {
        val other = profile.copy(id = VpnProfileId("other"), name = "Other")
        val profiles = InMemoryVpnProfileRepository(listOf(profile, other))
        val viewModel = HomeViewModel(
            fakeVpn(profiles),
            profiles,
            elapsedRealtimeMillis = { testScheduler.currentTime },
        )
        collectState(viewModel)

        viewModel.onAction(HomeAction.SelectProfile(other.id))
        runCurrent()

        assertEquals(other.id, viewModel.uiState.value.selectedProfileId)
    }

    private fun TestScope.collectState(viewModel: HomeViewModel) {
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            viewModel.uiState.collect()
        }
        runCurrent()
    }

    private fun TestScope.fakeVpn(profiles: InMemoryVpnProfileRepository) = FakeVpnRepository(
        profiles = profiles,
        scope = backgroundScope,
        timing = FakeVpnTiming(
            startupStepMillis = 1,
            reconnectMillis = 1,
            statisticsTickMillis = 1_000,
        ),
        elapsedRealtimeMillis = { testScheduler.currentTime },
    )
}
