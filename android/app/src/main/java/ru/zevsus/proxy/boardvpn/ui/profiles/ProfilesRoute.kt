package ru.zevsus.proxy.boardvpn.ui.profiles

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import ru.zevsus.proxy.boardvpn.R

@Composable
fun ProfilesRoute(
    viewModel: ProfilesViewModel,
    snackbarHostState: SnackbarHostState,
    contentPadding: PaddingValues,
    onClipboardImportRequest: () -> Unit,
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()

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
            } else {
                viewModel.onAction(action)
            }
        },
        contentPadding = contentPadding,
    )
}

@Composable
private fun ProfilesMessage.text(): String = when (this) {
    ProfilesMessage.ClipboardEmpty -> stringResource(R.string.profiles_error_clipboard_empty)
    ProfilesMessage.InvalidKeylink -> stringResource(R.string.profiles_error_invalid_keylink)
    ProfilesMessage.ProfileImported -> stringResource(R.string.profiles_imported)
    ProfilesMessage.ProfileDeleted -> stringResource(R.string.profiles_deleted)
}
