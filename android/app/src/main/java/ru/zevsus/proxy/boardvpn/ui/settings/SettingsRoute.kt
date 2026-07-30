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
    onOpenAppRouting: () -> Unit,
    appVersion: String,
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()

    SettingsScreen(
        state = state,
        onAction = { action ->
            when (action) {
                SettingsAction.OpenSystemVpnSettings -> onOpenSystemVpnSettings()
                SettingsAction.OpenAppRouting -> onOpenAppRouting()
                else -> viewModel.onAction(action)
            }
        },
        appVersion = appVersion,
        contentPadding = contentPadding,
    )
}
