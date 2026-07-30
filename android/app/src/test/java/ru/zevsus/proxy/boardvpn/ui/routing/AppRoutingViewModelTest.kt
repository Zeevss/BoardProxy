package ru.zevsus.proxy.boardvpn.ui.routing

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingMode
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingPolicy
import ru.zevsus.proxy.boardvpn.domain.model.AppSettings
import ru.zevsus.proxy.boardvpn.domain.model.InstalledApplication
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode
import ru.zevsus.proxy.boardvpn.domain.model.VpnConnectResult
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState
import ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics
import ru.zevsus.proxy.boardvpn.domain.repository.AppSettingsRepository
import ru.zevsus.proxy.boardvpn.domain.repository.InstalledApplicationsRepository
import ru.zevsus.proxy.boardvpn.domain.repository.VpnRepository
import ru.zevsus.proxy.boardvpn.test.MainDispatcherRule

@OptIn(ExperimentalCoroutinesApi::class)
class AppRoutingViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun `selecting an app completes a drafted routing mode`() = runTest {
        val settings = RoutingSettingsRepository()
        val vpn = RoutingVpnRepository()
        val viewModel = AppRoutingViewModel(
            settingsRepository = settings,
            applicationsRepository = InstalledApplicationsRepository {
                listOf(InstalledApplication("com.example.video", "Video"))
            },
            vpnRepository = vpn,
        )
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            viewModel.uiState.collect()
        }
        runCurrent()

        viewModel.onAction(
            AppRoutingAction.SelectMode(AppRoutingMode.OnlySelectedApps)
        )
        viewModel.onAction(AppRoutingAction.ToggleApplication("com.example.video"))
        runCurrent()

        assertEquals(
            AppRoutingPolicy(
                mode = AppRoutingMode.OnlySelectedApps,
                packageNames = setOf("com.example.video"),
            ),
            settings.value.appRoutingPolicy,
        )
    }

    @Test
    fun `removing the last selected app safely restores all-app routing`() = runTest {
        val policy = AppRoutingPolicy(
            mode = AppRoutingMode.ExcludeSelectedApps,
            packageNames = setOf("com.example.direct"),
        )
        val settings = RoutingSettingsRepository(AppSettings(appRoutingPolicy = policy))
        val viewModel = AppRoutingViewModel(
            settingsRepository = settings,
            applicationsRepository = InstalledApplicationsRepository { emptyList() },
            vpnRepository = RoutingVpnRepository(),
        )
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            viewModel.uiState.collect()
        }
        runCurrent()

        viewModel.onAction(AppRoutingAction.ToggleApplication("com.example.direct"))
        runCurrent()

        assertEquals(AppRoutingPolicy.AllApps, settings.value.appRoutingPolicy)
    }

    @Test
    fun `selecting all applications preserves the configured list and mode`() = runTest {
        val policy = AppRoutingPolicy(
            mode = AppRoutingMode.OnlySelectedApps,
            packageNames = setOf("com.example.video", "com.example.chat"),
        )
        val settings = RoutingSettingsRepository(AppSettings(appRoutingPolicy = policy))
        val viewModel = AppRoutingViewModel(
            settingsRepository = settings,
            applicationsRepository = InstalledApplicationsRepository { emptyList() },
            vpnRepository = RoutingVpnRepository(),
        )
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            viewModel.uiState.collect()
        }
        runCurrent()

        viewModel.onAction(AppRoutingAction.SelectMode(AppRoutingMode.AllApps))
        runCurrent()

        assertEquals(
            policy.copy(allProxy = true),
            settings.value.appRoutingPolicy,
        )
    }

    @Test
    fun `select all and clear update the complete application selection`() = runTest {
        val settings = RoutingSettingsRepository()
        val viewModel = AppRoutingViewModel(
            settingsRepository = settings,
            applicationsRepository = InstalledApplicationsRepository {
                listOf(
                    InstalledApplication("com.example.video", "Video"),
                    InstalledApplication("com.example.chat", "Chat"),
                )
            },
            vpnRepository = RoutingVpnRepository(),
        )
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            viewModel.uiState.collect()
        }
        runCurrent()

        viewModel.onAction(AppRoutingAction.SelectAllApplications)
        runCurrent()

        assertEquals(
            setOf("com.example.video", "com.example.chat"),
            settings.value.appRoutingPolicy.packageNames,
        )

        viewModel.onAction(AppRoutingAction.ClearApplicationSelection)
        runCurrent()

        assertEquals(AppRoutingPolicy.AllApps, settings.value.appRoutingPolicy)
    }

    @Test
    fun `active vpn offers restart after routing changes`() = runTest {
        val settings = RoutingSettingsRepository()
        val vpn = RoutingVpnRepository(
            VpnSessionState.Active(
                sessionId = VpnSessionId(1),
                profileId = VpnProfileId("profile"),
                phase = VpnSessionPhase.Connected,
                connectedAtElapsedRealtimeMillis = 1_000,
                appliedAppRoutingPolicy = AppRoutingPolicy.AllApps,
            )
        )
        val viewModel = AppRoutingViewModel(
            settingsRepository = settings,
            applicationsRepository = InstalledApplicationsRepository {
                listOf(InstalledApplication("com.example.video", "Video"))
            },
            vpnRepository = vpn,
        )
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            viewModel.uiState.collect()
        }
        runCurrent()

        viewModel.onAction(
            AppRoutingAction.SelectMode(AppRoutingMode.OnlySelectedApps)
        )
        viewModel.onAction(AppRoutingAction.ToggleApplication("com.example.video"))
        runCurrent()

        assertEquals(true, viewModel.uiState.value.restartRequired)

        viewModel.onAction(AppRoutingAction.RestartProxy)
        runCurrent()

        assertEquals(1, vpn.restartCalls)
    }
}

private class RoutingVpnRepository(
    initialSession: VpnSessionState = VpnSessionState.Idle,
) : VpnRepository {
    private val session = MutableStateFlow(initialSession)
    private val statistics = MutableStateFlow(VpnStatistics.Empty)
    var restartCalls: Int = 0
        private set

    override fun observeSession(): Flow<VpnSessionState> = session

    override fun observeStatistics(): Flow<VpnStatistics> = statistics

    override suspend fun connect(profileId: VpnProfileId): VpnConnectResult =
        VpnConnectResult.AlreadyRunning

    override suspend fun disconnect() = Unit

    override suspend fun restart() {
        restartCalls += 1
    }
}

private class RoutingSettingsRepository(
    initialValue: AppSettings = AppSettings.Default,
) : AppSettingsRepository {
    private val settings = MutableStateFlow(initialValue)
    val value: AppSettings
        get() = settings.value

    override fun observeSettings(): Flow<AppSettings> = settings

    override suspend fun setThemeMode(mode: ThemeMode) {
        settings.update { it.copy(themeMode = mode) }
    }

    override suspend fun setAutoConnectOnLaunch(enabled: Boolean) {
        settings.update { it.copy(autoConnectOnLaunch = enabled) }
    }

    override suspend fun setAppRoutingPolicy(policy: AppRoutingPolicy) {
        settings.update { it.copy(appRoutingPolicy = policy) }
    }
}
