package ru.zevsus.proxy.boardvpn.ui.home

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.RadioButton
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.ui.components.formatFingerprint

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeRoute(
    viewModel: HomeViewModel,
    snackbarHostState: SnackbarHostState,
    contentPadding: PaddingValues,
    onConnectRequest: () -> Unit,
    onManageProfiles: () -> Unit,
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    var selectorVisible by remember { mutableStateOf(false) }
    val sheetState = rememberModalBottomSheetState()

    val problemMessage = state.problem?.message()
    LaunchedEffect(problemMessage) {
        if (problemMessage != null) {
            snackbarHostState.showSnackbar(problemMessage)
            viewModel.onAction(HomeAction.DismissProblem)
        }
    }

    HomeScreen(
        state = state,
        onAction = { action ->
            if (action is HomeAction.ToggleConnection && state.canConnect) {
                onConnectRequest()
            } else {
                viewModel.onAction(action)
            }
        },
        onProfileSelectorClick = { selectorVisible = true },
        contentPadding = contentPadding,
    )

    if (selectorVisible) {
        ModalBottomSheet(
            onDismissRequest = { selectorVisible = false },
            sheetState = sheetState,
        ) {
            ProfileSelectorSheet(
                profiles = state.profiles,
                selectedProfileId = state.selectedProfileId,
                onProfileClick = { profile ->
                    viewModel.onAction(HomeAction.SelectProfile(profile.id))
                    selectorVisible = false
                },
                onManageProfiles = {
                    selectorVisible = false
                    onManageProfiles()
                },
            )
        }
    }
}

@Composable
private fun ProfileSelectorSheet(
    profiles: List<VpnProfile>,
    selectedProfileId: VpnProfileId?,
    onProfileClick: (VpnProfile) -> Unit,
    onManageProfiles: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .navigationBarsPadding()
            .padding(bottom = 16.dp),
    ) {
        Text(
            text = stringResource(R.string.home_select_profile),
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp),
        )

        if (profiles.isEmpty()) {
            Text(
                text = stringResource(R.string.profiles_empty_title),
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 24.dp, vertical = 12.dp),
            )
        }

        profiles.forEach { profile ->
            Surface(
                onClick = { onProfileClick(profile) },
                color = MaterialTheme.colorScheme.surface,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Row(
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    RadioButton(
                        selected = profile.id == selectedProfileId,
                        onClick = { onProfileClick(profile) },
                    )
                    Column {
                        Text(profile.name, style = MaterialTheme.typography.titleSmall)
                        Text(
                            text = formatFingerprint(profile.keylink.fingerprint()),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }

        TextButton(
            onClick = onManageProfiles,
            modifier = Modifier.padding(horizontal = 16.dp),
        ) {
            Text(stringResource(R.string.home_manage_profiles))
        }
    }
}

@Composable
private fun HomeProblem.message(): String = when (this) {
    HomeProblem.ProfileNotFound -> stringResource(R.string.home_error_profile_not_found)
    HomeProblem.PermissionDenied -> stringResource(R.string.home_error_permission_denied)
    is HomeProblem.SessionFailed -> stringResource(R.string.home_error_session_failed)
}
