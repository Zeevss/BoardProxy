package ru.zevsus.proxy.boardvpn.ui.settings

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle

@Composable
fun SettingsRoute(
    viewModel: SettingsViewModel,
    contentPadding: PaddingValues,
    onOpenSystemVpnSettings: () -> Unit,
    appVersion: String,
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()

    SettingsScreen(
        state = state,
        onAction = { action ->
            if (action is SettingsAction.OpenSystemVpnSettings) {
                onOpenSystemVpnSettings()
            } else {
                viewModel.onAction(action)
            }
        },
        appVersion = appVersion,
        contentPadding = contentPadding,
    )
}
