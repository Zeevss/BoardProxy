package ru.zevsus.proxy.boardvpn.ui.settings

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
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingPolicy
import ru.zevsus.proxy.boardvpn.domain.model.AppSettings
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode
import ru.zevsus.proxy.boardvpn.domain.repository.AppSettingsRepository
import ru.zevsus.proxy.boardvpn.test.MainDispatcherRule

@OptIn(ExperimentalCoroutinesApi::class)
class SettingsViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun `settings changes are persisted and observed back`() = runTest {
        val repository = InMemoryAppSettingsRepository()
        val viewModel = SettingsViewModel(repository)
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            viewModel.uiState.collect()
        }
        runCurrent()

        viewModel.onAction(SettingsAction.ChangeThemeMode(ThemeMode.Dark))
        viewModel.onAction(SettingsAction.ChangeAutoConnect(true))
        runCurrent()

        assertEquals(
            AppSettings(
                themeMode = ThemeMode.Dark,
                autoConnectOnLaunch = true,
            ),
            viewModel.uiState.value.settings,
        )
    }
}

private class InMemoryAppSettingsRepository : AppSettingsRepository {
    private val settings = MutableStateFlow(AppSettings.Default)

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
