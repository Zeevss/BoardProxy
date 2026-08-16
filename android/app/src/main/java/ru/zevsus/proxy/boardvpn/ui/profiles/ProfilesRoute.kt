package ru.zevsus.proxy.boardvpn.ui.profiles

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.ui.scanner.QrScannerDialog
import ru.zevsus.proxy.boardvpn.ui.scanner.QrShareDialog

@Composable
fun ProfilesRoute(
    viewModel: ProfilesViewModel,
    snackbarHostState: SnackbarHostState,
    contentPadding: PaddingValues,
    onClipboardImportRequest: () -> Unit,
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    var scannerVisible by remember { mutableStateOf(false) }

    val message = state.message?.text()
    LaunchedEffect(message) {
        if (message != null) {
            snackbarHostState.showSnackbar(message)
            viewModel.onAction(ProfilesAction.DismissMessage)
        }
    }

    ProfilesScreen(
        state = state,
        onAction = { action ->
            if (action is ProfilesAction.ImportFromClipboard) {
                onClipboardImportRequest()
            } else if (action is ProfilesAction.ScanQr) {
                scannerVisible = true
            } else {
                viewModel.onAction(action)
            }
        },
        contentPadding = contentPadding,
    )

    if (scannerVisible) {
        QrScannerDialog(
            onResult = { value ->
                scannerVisible = false
                viewModel.importLink(value)
            },
            onDismiss = { scannerVisible = false },
        )
    }
    state.profileForSharing?.let { profile ->
        QrShareDialog(
            title = profile.name,
            value = profile.shareValue(),
            onDismiss = { viewModel.onAction(ProfilesAction.DismissShare) },
        )
    }
}

@Composable
private fun ProfilesMessage.text(): String = when (this) {
    ProfilesMessage.ClipboardEmpty -> stringResource(R.string.profiles_error_clipboard_empty)
    ProfilesMessage.InvalidLink -> stringResource(R.string.profiles_error_invalid_link)
    ProfilesMessage.SubscriptionFailed -> stringResource(R.string.profiles_error_subscription)
    ProfilesMessage.ProfileImported -> stringResource(R.string.profiles_imported)
    ProfilesMessage.ProfileDeleted -> stringResource(R.string.profiles_deleted)
    ProfilesMessage.SubscriptionsUpdated -> stringResource(R.string.profiles_updated)
    ProfilesMessage.SubscriptionUpdateFailed -> stringResource(R.string.profiles_update_failed)
}
