package ru.zevsus.proxy.boardvpn.ui.settings

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.domain.model.AppSettings
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode
import ru.zevsus.proxy.boardvpn.ui.theme.BoardVPNTheme
import ru.zevsus.proxy.boardvpn.ui.theme.dynamicColorAvailable

@Composable
fun SettingsScreen(
    state: SettingsUiState,
    onAction: (SettingsAction) -> Unit,
    appVersion: String,
    modifier: Modifier = Modifier,
    contentPadding: PaddingValues = PaddingValues(),
    showDynamicColor: Boolean = dynamicColorAvailable,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(contentPadding)
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = stringResource(R.string.settings_title),
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.padding(start = 8.dp, top = 16.dp),
        )

        SettingsSection(title = stringResource(R.string.settings_section_appearance)) {
            Column(
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text(
                    text = stringResource(R.string.settings_theme),
                    style = MaterialTheme.typography.titleSmall,
                )

                SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                    ThemeMode.entries.forEachIndexed { index, mode ->
                        SegmentedButton(
                            selected = state.settings.themeMode == mode,
                            onClick = { onAction(SettingsAction.ChangeThemeMode(mode)) },
                            shape = SegmentedButtonDefaults.itemShape(
                                index = index,
                                count = ThemeMode.entries.size,
                            ),
                        ) {
                            Text(mode.label())
                        }
                    }
                }

                AnimatedVisibility(visible = showDynamicColor) {
                    SettingsToggleRow(
                        title = stringResource(R.string.settings_dynamic_color),
                        subtitle = stringResource(R.string.settings_dynamic_color_hint),
                        checked = state.settings.dynamicColor,
                        onCheckedChange = {
                            onAction(SettingsAction.ChangeDynamicColor(it))
                        },
                    )
                }
            }
        }

        SettingsSection(title = stringResource(R.string.settings_section_connection)) {
            SettingsToggleRow(
                title = stringResource(R.string.settings_auto_connect),
                subtitle = stringResource(R.string.settings_auto_connect_hint),
                checked = state.settings.autoConnectOnLaunch,
                onCheckedChange = { onAction(SettingsAction.ChangeAutoConnect(it)) },
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 12.dp),
            )

            HorizontalDivider()

            TextButton(
                onClick = { onAction(SettingsAction.OpenSystemVpnSettings) },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 12.dp, vertical = 4.dp),
            ) {
                Text(
                    text = stringResource(R.string.settings_system_vpn),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }

        SettingsSection(title = stringResource(R.string.settings_section_about)) {
            Column(
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 16.dp),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Text(
                    text = stringResource(R.string.app_name),
                    style = MaterialTheme.typography.titleSmall,
                )
                Text(
                    text = stringResource(R.string.settings_version, appVersion),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = stringResource(R.string.settings_core),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun SettingsSection(
    title: String,
    content: @Composable () -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(
            text = title,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(start = 8.dp),
        )
        Surface(
            shape = RoundedCornerShape(20.dp),
            color = MaterialTheme.colorScheme.surfaceVariant,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Column { content() }
        }
    }
}

@Composable
private fun SettingsToggleRow(
    title: String,
    subtitle: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(text = title, style = MaterialTheme.typography.titleSmall)
            Text(
                text = subtitle,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Switch(checked = checked, onCheckedChange = onCheckedChange)
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
    BoardVPNTheme(dynamicColor = false) {
        SettingsScreen(
            state = SettingsUiState(AppSettings(autoConnectOnLaunch = true)),
            onAction = {},
            appVersion = "1.0",
            showDynamicColor = true,
        )
    }
}

@Preview(showBackground = true)
@Composable
private fun SettingsDarkPreview() {
    BoardVPNTheme(themeMode = ThemeMode.Dark, dynamicColor = false) {
        SettingsScreen(
            state = SettingsUiState(AppSettings(themeMode = ThemeMode.Dark)),
            onAction = {},
            appVersion = "1.0",
            showDynamicColor = true,
        )
    }
}
