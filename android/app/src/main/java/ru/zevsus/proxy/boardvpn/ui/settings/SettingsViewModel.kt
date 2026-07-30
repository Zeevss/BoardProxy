package ru.zevsus.proxy.boardvpn.ui.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import ru.zevsus.proxy.boardvpn.domain.model.AppSettings
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode
import ru.zevsus.proxy.boardvpn.domain.repository.AppSettingsRepository

data class SettingsUiState(
    val settings: AppSettings = AppSettings.Default,
)

sealed interface SettingsAction {
    data class ChangeThemeMode(val mode: ThemeMode) : SettingsAction
    data class ChangeDynamicColor(val enabled: Boolean) : SettingsAction
    data class ChangeAutoConnect(val enabled: Boolean) : SettingsAction
    data object OpenSystemVpnSettings : SettingsAction
}

class SettingsViewModel(
    private val settingsRepository: AppSettingsRepository,
) : ViewModel() {
    val uiState = settingsRepository.observeSettings()
        .map(::SettingsUiState)
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000),
            initialValue = SettingsUiState(),
        )

    fun onAction(action: SettingsAction) {
        when (action) {
            is SettingsAction.ChangeThemeMode -> viewModelScope.launch {
                settingsRepository.setThemeMode(action.mode)
            }
            is SettingsAction.ChangeDynamicColor -> viewModelScope.launch {
                settingsRepository.setDynamicColor(action.enabled)
            }
            is SettingsAction.ChangeAutoConnect -> viewModelScope.launch {
                settingsRepository.setAutoConnectOnLaunch(action.enabled)
            }
            SettingsAction.OpenSystemVpnSettings -> Unit // handled by the hosting Activity
        }
    }
}
