package ru.zevsus.proxy.boardvpn.ui.routing

import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingMode
import ru.zevsus.proxy.boardvpn.domain.model.InstalledApplication
import ru.zevsus.proxy.boardvpn.ui.components.BoardVpnPill

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppRoutingScreen(
    state: AppRoutingUiState,
    onAction: (AppRoutingAction) -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Scaffold(
        modifier = modifier.fillMaxSize(),
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.routing_title)) },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(
                            Icons.AutoMirrored.Filled.ArrowBack,
                            contentDescription = stringResource(R.string.action_back),
                        )
                    }
                },
            )
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                Text(
                    text = stringResource(R.string.routing_mode_title),
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.padding(start = 4.dp, top = 8.dp),
                )
            }
            item {
                RoutingModeCard(
                    selected = state.allProxy,
                    title = stringResource(R.string.routing_mode_all),
                    subtitle = stringResource(R.string.routing_mode_all_hint),
                    onClick = {
                        onAction(AppRoutingAction.SelectMode(AppRoutingMode.AllApps))
                    },
                )
            }
            item {
                RoutingModeCard(
                    selected = !state.allProxy &&
                        state.mode == AppRoutingMode.ExcludeSelectedApps,
                    title = stringResource(R.string.routing_mode_bypass),
                    subtitle = stringResource(R.string.routing_mode_bypass_hint),
                    onClick = {
                        onAction(
                            AppRoutingAction.SelectMode(AppRoutingMode.ExcludeSelectedApps)
                        )
                    },
                )
            }
            item {
                RoutingModeCard(
                    selected = !state.allProxy &&
                        state.mode == AppRoutingMode.OnlySelectedApps,
                    title = stringResource(R.string.routing_mode_proxy_only),
                    subtitle = stringResource(R.string.routing_mode_proxy_only_hint),
                    onClick = {
                        onAction(
                            AppRoutingAction.SelectMode(AppRoutingMode.OnlySelectedApps)
                        )
                    },
                )
            }
            if (state.restartRequired) {
                item {
                    FilledTonalButton(
                        onClick = { onAction(AppRoutingAction.RestartProxy) },
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Text(stringResource(R.string.routing_restart_proxy))
                    }
                }
            }
            if (!state.allProxy && state.selectedCount == 0) {
                item {
                    Text(
                        text = stringResource(R.string.routing_select_app_hint),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.padding(horizontal = 8.dp),
                    )
                }
            }
            item {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(start = 4.dp, top = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = stringResource(R.string.routing_apps_title),
                        style = MaterialTheme.typography.labelLarge,
                        color = MaterialTheme.colorScheme.primary,
                    )
                    Spacer(Modifier.weight(1f))
                    BoardVpnPill(
                        text = pluralStringResource(
                            R.plurals.routing_selected_count,
                            state.selectedCount,
                            state.selectedCount,
                        )
                    )
                }
            }
            item {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.End,
                ) {
                    TextButton(
                        onClick = {
                            onAction(AppRoutingAction.SelectAllApplications)
                        },
                        enabled = state.applications.any(InstalledApplication::installed),
                    ) {
                        Text(stringResource(R.string.routing_select_all))
                    }
                    TextButton(
                        onClick = {
                            onAction(AppRoutingAction.ClearApplicationSelection)
                        },
                        enabled = state.selectedCount > 0,
                    ) {
                        Text(stringResource(R.string.routing_clear_selection))
                    }
                }
            }
            item {
                OutlinedTextField(
                    value = state.query,
                    onValueChange = { onAction(AppRoutingAction.Search(it)) },
                    leadingIcon = {
                        Icon(Icons.Default.Search, contentDescription = null)
                    },
                    placeholder = { Text(stringResource(R.string.routing_search)) },
                    singleLine = true,
                    shape = RoundedCornerShape(18.dp),
                    modifier = Modifier.fillMaxWidth(),
                )
            }

            if (state.loading) {
                item {
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(40.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        CircularProgressIndicator()
                    }
                }
            } else if (state.visibleApplications.isEmpty()) {
                item {
                    Text(
                        text = stringResource(R.string.routing_apps_empty),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(20.dp),
                    )
                }
            } else {
                items(
                    items = state.visibleApplications,
                    key = InstalledApplication::packageName,
                ) { application ->
                    ApplicationRow(
                        application = application,
                        selected = application.packageName in state.selectedPackageNames,
                        onClick = {
                            onAction(
                                AppRoutingAction.ToggleApplication(application.packageName)
                            )
                        },
                    )
                }
            }
        }
    }
}

@Composable
private fun RoutingModeCard(
    selected: Boolean,
    title: String,
    subtitle: String,
    onClick: () -> Unit,
) {
    Surface(
        onClick = onClick,
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(22.dp),
        color = if (selected) {
            MaterialTheme.colorScheme.primaryContainer
        } else {
            MaterialTheme.colorScheme.surfaceContainer
        },
        border = if (selected) {
            androidx.compose.foundation.BorderStroke(1.dp, MaterialTheme.colorScheme.primary)
        } else {
            null
        },
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 14.dp, vertical = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            RadioButton(selected = selected, onClick = onClick)
            Column(modifier = Modifier.weight(1f)) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(title, style = MaterialTheme.typography.titleSmall)
                }
                Text(
                    subtitle,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun ApplicationRow(
    application: InstalledApplication,
    selected: Boolean,
    onClick: () -> Unit,
) {
    Surface(
        onClick = onClick,
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(18.dp),
        color = MaterialTheme.colorScheme.surfaceContainer,
    ) {
        Row(
            modifier = Modifier.padding(start = 14.dp, end = 8.dp, top = 10.dp, bottom = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            ApplicationIcon(application)
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = application.label,
                    style = MaterialTheme.typography.titleSmall,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = if (application.installed) {
                        application.packageName
                    } else {
                        stringResource(
                            R.string.routing_app_not_installed,
                            application.packageName,
                        )
                    },
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Checkbox(
                checked = selected,
                onCheckedChange = { onClick() },
            )
        }
    }
}

@Composable
private fun ApplicationIcon(application: InstalledApplication) {
    val packageManager = LocalContext.current.packageManager
    val image by produceState<ImageBitmap?>(
        initialValue = null,
        key1 = application.packageName,
        key2 = application.installed,
    ) {
        value = if (application.installed) {
            withContext(Dispatchers.IO) {
                runCatching {
                    val drawable = packageManager.getApplicationIcon(application.packageName)
                    val bitmap = Bitmap.createBitmap(
                        APPLICATION_ICON_SIZE,
                        APPLICATION_ICON_SIZE,
                        Bitmap.Config.ARGB_8888,
                    )
                    drawable.setBounds(0, 0, bitmap.width, bitmap.height)
                    drawable.draw(Canvas(bitmap))
                    bitmap.asImageBitmap()
                }.getOrNull()
            }
        } else {
            null
        }
    }

    Surface(
        modifier = Modifier.size(42.dp),
        shape = CircleShape,
        color = MaterialTheme.colorScheme.secondaryContainer,
        contentColor = MaterialTheme.colorScheme.onSecondaryContainer,
    ) {
        if (image != null) {
            Image(
                bitmap = checkNotNull(image),
                contentDescription = null,
                contentScale = ContentScale.Fit,
                modifier = Modifier.padding(4.dp),
            )
        } else {
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) {
                Text(
                    text = application.label.firstOrNull()?.uppercase() ?: "•",
                    style = MaterialTheme.typography.titleMedium,
                )
            }
        }
    }
}

private const val APPLICATION_ICON_SIZE = 96
