package ru.zevsus.proxy.boardvpn.ui.profiles

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.ui.components.BoardVpnPageHeader
import ru.zevsus.proxy.boardvpn.ui.components.BoardVpnShieldOutline
import ru.zevsus.proxy.boardvpn.ui.components.formatFingerprint
import ru.zevsus.proxy.boardvpn.ui.theme.BoardVPNTheme
import ru.zevsus.proxy.boardvpn.ui.theme.LocalConnectionColors

@Composable
fun ProfilesScreen(
    state: ProfilesUiState,
    onAction: (ProfilesAction) -> Unit,
    modifier: Modifier = Modifier,
    contentPadding: PaddingValues = PaddingValues(),
) {
    Box(
        modifier = modifier
            .fillMaxSize()
            .padding(contentPadding),
    ) {
        Column(modifier = Modifier.fillMaxSize()) {
            BoardVpnPageHeader(
                title = stringResource(R.string.profiles_title),
                subtitle = stringResource(R.string.profiles_subtitle),
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 20.dp),
            )

            if (state.profiles.isEmpty()) {
                EmptyProfiles(
                    onImportClick = { onAction(ProfilesAction.ImportFromClipboard) },
                    modifier = Modifier.weight(1f),
                )
            } else {
                LazyColumn(
                    modifier = Modifier.weight(1f),
                    contentPadding = PaddingValues(
                        start = 16.dp,
                        end = 16.dp,
                        bottom = 96.dp,
                    ),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    items(state.profiles, key = { it.id.value }) { profile ->
                        ProfileCard(
                            profile = profile,
                            selected = profile.id == state.selectedProfileId,
                            onClick = { onAction(ProfilesAction.SelectProfile(profile.id)) },
                            onEdit = { onAction(ProfilesAction.EditProfile(profile.id)) },
                            onDelete = { onAction(ProfilesAction.RequestDeletion(profile.id)) },
                            modifier = Modifier.animateItem(),
                        )
                    }

                    item {
                        OutlinedButton(
                            onClick = { onAction(ProfilesAction.ImportFromClipboard) },
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(top = 4.dp),
                        ) {
                            Text(stringResource(R.string.profiles_import_clipboard))
                        }
                    }
                }
            }
        }

        ExtendedFloatingActionButton(
            onClick = { onAction(ProfilesAction.AddProfile) },
            icon = { Icon(Icons.Default.Add, contentDescription = null) },
            text = { Text(stringResource(R.string.profiles_add)) },
            modifier = Modifier
                .align(Alignment.BottomEnd)
                .padding(24.dp),
        )
    }

    state.editor?.let { editor ->
        ProfileEditorDialog(
            state = editor,
            onNameChange = { onAction(ProfilesAction.EditorNameChanged(it)) },
            onKeylinkChange = { onAction(ProfilesAction.EditorKeylinkChanged(it)) },
            onConfirm = { onAction(ProfilesAction.SaveEditor) },
            onDismiss = { onAction(ProfilesAction.DismissEditor) },
        )
    }

    state.profilePendingDeletion?.let { profile ->
        AlertDialog(
            onDismissRequest = { onAction(ProfilesAction.DismissDeletion) },
            title = { Text(stringResource(R.string.profiles_delete_title)) },
            text = { Text(stringResource(R.string.profiles_delete_message, profile.name)) },
            confirmButton = {
                TextButton(onClick = { onAction(ProfilesAction.ConfirmDeletion) }) {
                    Text(stringResource(R.string.action_delete))
                }
            },
            dismissButton = {
                TextButton(onClick = { onAction(ProfilesAction.DismissDeletion) }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }
}

@Composable
private fun ProfileCard(
    profile: VpnProfile,
    selected: Boolean,
    onClick: () -> Unit,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Surface(
        onClick = onClick,
        shape = RoundedCornerShape(20.dp),
        color = if (selected) {
            MaterialTheme.colorScheme.primaryContainer
        } else {
            MaterialTheme.colorScheme.surfaceContainer
        },
        modifier = modifier.fillMaxWidth(),
    ) {
        Row(
            modifier = Modifier.padding(start = 20.dp, top = 12.dp, end = 8.dp, bottom = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = profile.name,
                        style = MaterialTheme.typography.titleSmall,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f, fill = false),
                    )
                    if (selected) {
                        Spacer(Modifier.width(8.dp))
                        Icon(
                            imageVector = Icons.Default.CheckCircle,
                            contentDescription = stringResource(R.string.profiles_active),
                            tint = LocalConnectionColors.current.connected,
                            modifier = Modifier.size(16.dp),
                        )
                    }
                }
                Text(
                    text = formatFingerprint(profile.keylink.fingerprint()),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            IconButton(onClick = onEdit) {
                Icon(
                    imageVector = Icons.Default.Edit,
                    contentDescription = stringResource(R.string.action_edit),
                )
            }
            IconButton(onClick = onDelete) {
                Icon(
                    imageVector = Icons.Default.Delete,
                    contentDescription = stringResource(R.string.action_delete),
                )
            }
        }
    }
}

@Composable
private fun EmptyProfiles(
    onImportClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            imageVector = BoardVpnShieldOutline,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.outline,
            modifier = Modifier.size(64.dp),
        )
        Spacer(Modifier.height(16.dp))
        Text(
            text = stringResource(R.string.profiles_empty_title),
            style = MaterialTheme.typography.titleMedium,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            text = stringResource(R.string.profiles_empty_hint),
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(24.dp))
        OutlinedButton(onClick = onImportClick) {
            Text(stringResource(R.string.profiles_import_clipboard))
        }
    }
}

private val previewProfiles = listOf(
    VpnProfile(
        id = VpnProfileId("amsterdam"),
        name = "Amsterdam node",
        keylink = BoardProxyKeylink.fromRaw("bproxy://${"A".repeat(86)}"),
    ),
    VpnProfile(
        id = VpnProfileId("berlin"),
        name = "Berlin node",
        keylink = BoardProxyKeylink.fromRaw("bproxy://${"B".repeat(86)}"),
    ),
)

@Preview(showBackground = true)
@Composable
private fun ProfilesPreview() {
    BoardVPNTheme {
        ProfilesScreen(
            state = ProfilesUiState(
                profiles = previewProfiles,
                selectedProfileId = previewProfiles.first().id,
            ),
            onAction = {},
        )
    }
}

@Preview(showBackground = true)
@Composable
private fun ProfilesEmptyPreview() {
    BoardVPNTheme(themeMode = ThemeMode.Dark) {
        ProfilesScreen(state = ProfilesUiState(), onAction = {})
    }
}
