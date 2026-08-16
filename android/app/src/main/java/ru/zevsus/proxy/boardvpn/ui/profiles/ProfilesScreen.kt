package ru.zevsus.proxy.boardvpn.ui.profiles

import android.text.format.DateFormat
import androidx.compose.foundation.border
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import java.util.Date
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.domain.model.SubscriptionKeySummary
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.ui.components.BoardVpnShieldOutline
import ru.zevsus.proxy.boardvpn.ui.components.formatBytes
import ru.zevsus.proxy.boardvpn.ui.components.formatFingerprint
import ru.zevsus.proxy.boardvpn.ui.theme.LocalConnectionColors

@Composable
fun ProfilesScreen(
    state: ProfilesUiState,
    onAction: (ProfilesAction) -> Unit,
    modifier: Modifier = Modifier,
    contentPadding: PaddingValues = PaddingValues(),
) {
    val subscriptions = state.profiles.filter { it.subscription != null }
    val directProfiles = state.profiles.filter { it.subscription == null }
    var addMenuVisible by remember { mutableStateOf(false) }

    Column(modifier = modifier.fillMaxSize().padding(contentPadding)) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(start = 20.dp, top = 20.dp, end = 12.dp, bottom = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(stringResource(R.string.profiles_title), style = MaterialTheme.typography.headlineMedium)
                Text(
                    stringResource(R.string.profiles_subtitle),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Box {
                IconButton(onClick = { addMenuVisible = true }) {
                    Icon(Icons.Default.Add, contentDescription = stringResource(R.string.profiles_add))
                }
                DropdownMenu(
                    expanded = addMenuVisible,
                    onDismissRequest = { addMenuVisible = false },
                ) {
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.profiles_scan_qr)) },
                        onClick = {
                            addMenuVisible = false
                            onAction(ProfilesAction.ScanQr)
                        },
                    )
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.profiles_paste)) },
                        onClick = {
                            addMenuVisible = false
                            onAction(ProfilesAction.ImportFromClipboard)
                        },
                    )
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.profiles_add_manually)) },
                        onClick = {
                            addMenuVisible = false
                            onAction(ProfilesAction.AddProfile)
                        },
                    )
                }
            }
        }

        if (state.profiles.isEmpty()) {
            EmptyProfiles(
                onAddClick = { onAction(ProfilesAction.AddProfile) },
                onScanClick = { onAction(ProfilesAction.ScanQr) },
                modifier = Modifier.weight(1f),
            )
        } else {
            LazyColumn(
                modifier = Modifier.weight(1f),
                contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 28.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (subscriptions.isNotEmpty()) {
                    item {
                        SectionHeader(
                            title = stringResource(R.string.profiles_subscriptions),
                            action = stringResource(R.string.profiles_refresh),
                            refreshing = state.refreshingSubscriptions.isNotEmpty(),
                            onAction = { onAction(ProfilesAction.RefreshSubscriptions) },
                        )
                    }
                    items(subscriptions, key = { it.id.value }) { profile ->
                        SubscriptionCard(
                            profile = profile,
                            selected = profile.id == state.selectedProfileId,
                            refreshing = profile.id in state.refreshingSubscriptions,
                            failed = profile.id in state.failedSubscriptions,
                            onSelect = { onAction(ProfilesAction.SelectProfile(profile.id)) },
                            onRefresh = { onAction(ProfilesAction.RefreshSubscription(profile.id)) },
                            onEdit = { onAction(ProfilesAction.EditProfile(profile.id)) },
                            onShare = { onAction(ProfilesAction.ShareProfile(profile.id)) },
                            onDelete = { onAction(ProfilesAction.RequestDeletion(profile.id)) },
                            modifier = Modifier.animateItem(),
                        )
                    }
                }
                if (directProfiles.isNotEmpty()) {
                    item { SectionHeader(stringResource(R.string.profiles_direct_keys)) }
                    items(directProfiles, key = { it.id.value }) { profile ->
                        DirectProfileCard(
                            profile = profile,
                            selected = profile.id == state.selectedProfileId,
                            onSelect = { onAction(ProfilesAction.SelectProfile(profile.id)) },
                            onEdit = { onAction(ProfilesAction.EditProfile(profile.id)) },
                            onShare = { onAction(ProfilesAction.ShareProfile(profile.id)) },
                            onDelete = { onAction(ProfilesAction.RequestDeletion(profile.id)) },
                            modifier = Modifier.animateItem(),
                        )
                    }
                }
            }
        }
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
private fun SectionHeader(
    title: String,
    action: String? = null,
    refreshing: Boolean = false,
    onAction: () -> Unit = {},
) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(start = 4.dp, top = 10.dp, end = 2.dp, bottom = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = title,
            style = MaterialTheme.typography.titleSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(1f),
        )
        if (action != null) {
            TextButton(onClick = onAction, enabled = !refreshing) {
                if (refreshing) {
                    CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                    Spacer(Modifier.width(8.dp))
                } else {
                    Icon(Icons.Default.Refresh, contentDescription = null, modifier = Modifier.size(18.dp))
                    Spacer(Modifier.width(6.dp))
                }
                Text(if (refreshing) stringResource(R.string.profiles_refreshing) else action)
            }
        }
    }
}

@Composable
private fun SubscriptionCard(
    profile: VpnProfile,
    selected: Boolean,
    refreshing: Boolean,
    failed: Boolean,
    onSelect: () -> Unit,
    onRefresh: () -> Unit,
    onEdit: () -> Unit,
    onShare: () -> Unit,
    onDelete: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val subscription = checkNotNull(profile.subscription)
    val shape = RoundedCornerShape(22.dp)
    Surface(
        onClick = onSelect,
        shape = shape,
        color = MaterialTheme.colorScheme.surfaceContainer,
        tonalElevation = if (selected) 2.dp else 0.dp,
        modifier = modifier
            .fillMaxWidth()
            .then(
                if (selected) Modifier.border(1.dp, MaterialTheme.colorScheme.primary, shape)
                else Modifier
            ),
    ) {
        Column {
            Row(
                modifier = Modifier.padding(start = 18.dp, top = 15.dp, end = 6.dp, bottom = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        profile.name,
                        style = MaterialTheme.typography.titleMedium,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        subscriptionStatus(subscription.updatedAtEpochMillis, refreshing, failed),
                        style = MaterialTheme.typography.bodySmall,
                        color = if (failed) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (selected) ActiveDot()
                ProfileMenu(onRefresh, onEdit, onShare, onDelete)
            }
            HorizontalDivider(color = MaterialTheme.colorScheme.outline.copy(alpha = 0.35f))
            subscription.keys.forEachIndexed { index, key ->
                if (index > 0) {
                    HorizontalDivider(
                        modifier = Modifier.padding(horizontal = 18.dp),
                        color = MaterialTheme.colorScheme.outline.copy(alpha = 0.22f),
                    )
                }
                SubscriptionKeyRow(key)
            }
        }
    }
}

@Composable
private fun subscriptionStatus(updatedAt: Long, refreshing: Boolean, failed: Boolean): String {
    if (refreshing) return stringResource(R.string.profiles_refreshing)
    if (failed) return stringResource(R.string.profiles_refresh_failed)
    if (updatedAt <= 0) return stringResource(R.string.profiles_not_refreshed)
    val time = DateFormat.getTimeFormat(LocalContext.current).format(Date(updatedAt))
    return stringResource(R.string.profiles_updated_at, time)
}

@Composable
private fun SubscriptionKeyRow(key: SubscriptionKeySummary) {
    val enabled = key.state == "enabled"
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 18.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Surface(
            color = if (enabled) LocalConnectionColors.current.connected else MaterialTheme.colorScheme.outline,
            shape = CircleShape,
            modifier = Modifier.size(8.dp),
        ) {}
        Spacer(Modifier.width(12.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(
                key.name.ifBlank { key.id },
                style = MaterialTheme.typography.titleSmall,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (key.nodeId.isNotBlank()) {
                Text(
                    key.nodeId,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        Text(
            formatBytes(key.usedBytes),
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun DirectProfileCard(
    profile: VpnProfile,
    selected: Boolean,
    onSelect: () -> Unit,
    onEdit: () -> Unit,
    onShare: () -> Unit,
    onDelete: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val shape = RoundedCornerShape(22.dp)
    Surface(
        onClick = onSelect,
        shape = shape,
        color = MaterialTheme.colorScheme.surfaceContainer,
        tonalElevation = if (selected) 2.dp else 0.dp,
        modifier = modifier
            .fillMaxWidth()
            .then(
                if (selected) Modifier.border(1.dp, MaterialTheme.colorScheme.primary, shape)
                else Modifier
            ),
    ) {
        Row(
            modifier = Modifier.padding(start = 18.dp, top = 14.dp, end = 6.dp, bottom = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(profile.name, style = MaterialTheme.typography.titleMedium, maxLines = 1)
                Text(
                    formatFingerprint(profile.keylink.fingerprint()),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (selected) ActiveDot()
            ProfileMenu(null, onEdit, onShare, onDelete)
        }
    }
}

@Composable
private fun ActiveDot() {
    Surface(
        color = LocalConnectionColors.current.connected,
        shape = CircleShape,
        modifier = Modifier.size(9.dp),
    ) {}
    Spacer(Modifier.width(4.dp))
}

@Composable
private fun ProfileMenu(
    onRefresh: (() -> Unit)?,
    onEdit: () -> Unit,
    onShare: () -> Unit,
    onDelete: () -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { expanded = true }) {
            Icon(Icons.Default.MoreVert, contentDescription = stringResource(R.string.profiles_more))
        }
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            if (onRefresh != null) {
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.profiles_refresh_one)) },
                    onClick = {
                        expanded = false
                        onRefresh()
                    },
                )
            }
            DropdownMenuItem(
                text = { Text(stringResource(R.string.action_edit)) },
                onClick = {
                    expanded = false
                    onEdit()
                },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.action_share)) },
                onClick = {
                    expanded = false
                    onShare()
                },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.action_delete), color = MaterialTheme.colorScheme.error) },
                onClick = {
                    expanded = false
                    onDelete()
                },
            )
        }
    }
}

@Composable
private fun EmptyProfiles(
    onAddClick: () -> Unit,
    onScanClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier.fillMaxWidth().padding(horizontal = 32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            BoardVpnShieldOutline,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.outline,
            modifier = Modifier.size(58.dp),
        )
        Spacer(Modifier.height(16.dp))
        Text(
            stringResource(R.string.profiles_empty_title),
            style = MaterialTheme.typography.titleMedium,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(6.dp))
        Text(
            stringResource(R.string.profiles_empty_hint),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(20.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TextButton(onClick = onScanClick) { Text(stringResource(R.string.profiles_scan_qr)) }
            TextButton(onClick = onAddClick) { Text(stringResource(R.string.profiles_add_manually)) }
        }
    }
}
