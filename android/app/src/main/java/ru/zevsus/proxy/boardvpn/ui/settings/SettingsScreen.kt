package ru.zevsus.proxy.boardvpn.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.MenuAnchorType
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingMode
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingPolicy
import ru.zevsus.proxy.boardvpn.domain.model.AppSettings
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode
import ru.zevsus.proxy.boardvpn.ui.components.BoardVpnNavigationRow
import ru.zevsus.proxy.boardvpn.ui.components.BoardVpnPageHeader
import ru.zevsus.proxy.boardvpn.ui.components.BoardVpnSection
import ru.zevsus.proxy.boardvpn.ui.theme.BoardVPNTheme

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    state: SettingsUiState,
    onAction: (SettingsAction) -> Unit,
    appVersion: String,
    modifier: Modifier = Modifier,
    contentPadding: PaddingValues = PaddingValues(),
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(contentPadding)
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp),
        verticalArrangement = Arrangement.spacedBy(20.dp),
    ) {
        BoardVpnPageHeader(
            title = stringResource(R.string.settings_title),
            subtitle = stringResource(R.string.settings_subtitle),
            modifier = Modifier.padding(top = 20.dp),
        )

        BoardVpnSection(title = stringResource(R.string.settings_section_connection)) {
            BoardVpnNavigationRow(
                title = stringResource(R.string.settings_app_routing),
                subtitle = state.settings.appRoutingPolicy.routingSummary(),
                onClick = { onAction(SettingsAction.OpenAppRouting) },
            )
            HorizontalDivider(modifier = Modifier.padding(horizontal = 18.dp))
            SettingsToggleRow(
                title = stringResource(R.string.settings_auto_connect),
                subtitle = stringResource(R.string.settings_auto_connect_hint),
                checked = state.settings.autoConnectOnLaunch,
                onCheckedChange = { onAction(SettingsAction.ChangeAutoConnect(it)) },
            )
            HorizontalDivider(modifier = Modifier.padding(horizontal = 18.dp))
            BoardVpnNavigationRow(
                title = stringResource(R.string.settings_system_vpn),
                subtitle = stringResource(R.string.settings_system_vpn_hint),
                onClick = { onAction(SettingsAction.OpenSystemVpnSettings) },
            )
        }

        BoardVpnSection(title = stringResource(R.string.settings_section_appearance)) {
            ThemeDropdown(
                selected = state.settings.themeMode,
                onSelected = { onAction(SettingsAction.ChangeThemeMode(it)) },
                modifier = Modifier.padding(horizontal = 18.dp, vertical = 16.dp),
            )
        }

        BoardVpnSection(title = stringResource(R.string.settings_section_about)) {
            Column(
                modifier = Modifier.padding(horizontal = 18.dp, vertical = 16.dp),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Text(
                    text = stringResource(R.string.app_name),
                    style = MaterialTheme.typography.titleSmall,
                )
                Text(
                    text = stringResource(R.string.settings_version, appVersion),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = stringResource(R.string.settings_core),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        androidx.compose.foundation.layout.Spacer(Modifier.padding(bottom = 12.dp))
    }
}

@Composable
@OptIn(ExperimentalMaterial3Api::class)
private fun ThemeDropdown(
    selected: ThemeMode,
    onSelected: (ThemeMode) -> Unit,
    modifier: Modifier = Modifier,
) {
    var expanded by remember { mutableStateOf(false) }

    ExposedDropdownMenuBox(
        expanded = expanded,
        onExpandedChange = { expanded = it },
        modifier = modifier,
    ) {
        OutlinedTextField(
            value = selected.label(),
            onValueChange = {},
            readOnly = true,
            label = { Text(stringResource(R.string.settings_theme)) },
            trailingIcon = {
                ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded)
            },
            modifier = Modifier
                .menuAnchor(MenuAnchorType.PrimaryNotEditable)
                .fillMaxWidth(),
        )
        ExposedDropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false },
        ) {
            ThemeMode.entries.forEach { mode ->
                DropdownMenuItem(
                    text = { Text(mode.label()) },
                    onClick = {
                        expanded = false
                        onSelected(mode)
                    },
                )
            }
        }
    }
}

@Composable
private fun SettingsToggleRow(
    title: String,
    subtitle: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    horizontalPadding: androidx.compose.ui.unit.Dp = 18.dp,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = horizontalPadding, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(text = title, style = MaterialTheme.typography.titleSmall)
            Text(
                text = subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Switch(checked = checked, onCheckedChange = onCheckedChange)
    }
}

@Composable
private fun AppRoutingPolicy.routingSummary(): String {
    if (allProxy) return stringResource(R.string.routing_mode_all)
    val selectedCount = packageNames.size
    return when (mode) {
        AppRoutingMode.AllApps -> stringResource(R.string.routing_mode_all)
        AppRoutingMode.ExcludeSelectedApps -> pluralStringResource(
            R.plurals.settings_routing_bypass_summary,
            selectedCount,
            selectedCount,
        )
        AppRoutingMode.OnlySelectedApps -> pluralStringResource(
            R.plurals.settings_routing_only_summary,
            selectedCount,
            selectedCount,
        )
    }
}

@Composable
private fun ThemeMode.label(): String = stringResource(
    when (this) {
        ThemeMode.System -> R.string.settings_theme_system
        ThemeMode.Light -> R.string.settings_theme_light
        ThemeMode.Dark -> R.string.settings_theme_dark
    }
)

@Preview(showBackground = true)
@Composable
private fun SettingsPreview() {
    BoardVPNTheme {
        SettingsScreen(
            state = SettingsUiState(AppSettings(autoConnectOnLaunch = true)),
            onAction = {},
            appVersion = "1.0",
        )
    }
}
