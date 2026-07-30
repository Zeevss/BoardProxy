package ru.zevsus.proxy.boardvpn.ui.home

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import kotlin.time.Duration.Companion.seconds
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics
import ru.zevsus.proxy.boardvpn.ui.components.BoardVpnShieldOutline
import ru.zevsus.proxy.boardvpn.ui.components.formatBytesPerSecond
import ru.zevsus.proxy.boardvpn.ui.components.formatDuration
import ru.zevsus.proxy.boardvpn.ui.components.formatFingerprint
import ru.zevsus.proxy.boardvpn.ui.theme.BoardVPNTheme
import ru.zevsus.proxy.boardvpn.ui.theme.LocalConnectionColors

private const val EMPTY_VALUE = "—"

@Composable
fun HomeScreen(
    state: HomeUiState,
    onAction: (HomeAction) -> Unit,
    onProfileSelectorClick: () -> Unit,
    modifier: Modifier = Modifier,
    contentPadding: PaddingValues = PaddingValues(),
) {
    BoxWithConstraints(
        modifier = modifier
            .fillMaxSize()
            .padding(contentPadding),
    ) {
        val shieldSize = minOf(maxWidth * 0.62f, maxHeight * 0.36f, 240.dp)

        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 24.dp, vertical = 16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Column(
                modifier = Modifier
                    .weight(1f)
                    .fillMaxWidth(),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) {
                ConnectionButton(
                    status = state.status,
                    enabled = state.canConnect || state.canDisconnect,
                    onClick = { onAction(HomeAction.ToggleConnection) },
                    size = shieldSize,
                )

                Spacer(Modifier.height(28.dp))

                AnimatedContent(
                    targetState = state.status,
                    transitionSpec = { fadeIn(tween(200)) togetherWith fadeOut(tween(200)) },
                    label = "status",
                ) { status ->
                    Text(
                        text = status.label(),
                        style = MaterialTheme.typography.headlineMedium,
                        color = status.color(),
                    )
                }

                Spacer(Modifier.height(6.dp))

                Text(
                    text = state.connectedDuration?.let(::formatDuration) ?: EMPTY_VALUE,
                    style = MaterialTheme.typography.headlineSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )

                Spacer(Modifier.height(32.dp))

                StatisticsRow(state)
            }

            ProfileSelectorCard(
                profileName = state.selectedProfile?.name,
                fingerprint = state.selectedProfile?.keylink?.fingerprint(),
                onClick = onProfileSelectorClick,
            )
        }
    }
}

@Composable
private fun StatisticsRow(state: HomeUiState) {
    val live = state.isSessionLive
    val statistics = state.statistics

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        StatisticColumn(
            label = stringResource(R.string.home_download),
            value = if (live) {
                formatBytesPerSecond(statistics.downloadBytesPerSecond)
            } else {
                EMPTY_VALUE
            },
        )
        StatisticColumn(
            label = stringResource(R.string.home_latency),
            value = statistics.roundTripTimeMillis
                ?.takeIf { live }
                ?.let { stringResource(R.string.home_latency_value, it) }
                ?: EMPTY_VALUE,
        )
        StatisticColumn(
            label = stringResource(R.string.home_upload),
            value = if (live) {
                formatBytesPerSecond(statistics.uploadBytesPerSecond)
            } else {
                EMPTY_VALUE
            },
        )
    }
}

@Composable
private fun RowScope.StatisticColumn(label: String, value: String) {
    Surface(
        modifier = Modifier.weight(1f),
        shape = RoundedCornerShape(18.dp),
        color = MaterialTheme.colorScheme.surfaceContainer,
    ) {
        Column(
            modifier = Modifier.padding(horizontal = 6.dp, vertical = 12.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            AnimatedContent(
                targetState = value,
                transitionSpec = { fadeIn(tween(180)) togetherWith fadeOut(tween(180)) },
                label = "statistic",
            ) { animatedValue ->
                Text(
                    text = animatedValue,
                    style = MaterialTheme.typography.titleSmall,
                    maxLines = 1,
                )
            }
            Text(
                text = label,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun ProfileSelectorCard(
    profileName: String?,
    fingerprint: String?,
    onClick: () -> Unit,
) {
    Surface(
        onClick = onClick,
        shape = RoundedCornerShape(20.dp),
        color = MaterialTheme.colorScheme.surfaceContainer,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 20.dp, vertical = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = BoardVpnShieldOutline,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.primary,
                modifier = Modifier.size(24.dp),
            )

            Spacer(Modifier.width(16.dp))

            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = profileName ?: stringResource(R.string.home_no_profile),
                    style = MaterialTheme.typography.titleSmall,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = fingerprint?.let(::formatFingerprint)
                        ?: stringResource(R.string.home_no_profile_hint),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }

            Text(
                text = stringResource(R.string.home_change_profile),
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.primary,
            )
        }
    }
}

@Composable
private fun HomeConnectionStatus.label(): String = stringResource(
    when (this) {
        HomeConnectionStatus.Disconnected -> R.string.home_status_disconnected
        HomeConnectionStatus.Connecting -> R.string.home_status_connecting
        HomeConnectionStatus.Connected -> R.string.home_status_connected
        HomeConnectionStatus.Reconnecting -> R.string.home_status_reconnecting
        HomeConnectionStatus.Disconnecting -> R.string.home_status_disconnecting
    }
)

@Composable
private fun HomeConnectionStatus.color(): Color = when (this) {
    HomeConnectionStatus.Connected -> LocalConnectionColors.current.connected
    HomeConnectionStatus.Reconnecting -> LocalConnectionColors.current.reconnecting
    else -> MaterialTheme.colorScheme.onSurface
}

private val previewProfile = VpnProfile(
    id = VpnProfileId("preview"),
    name = "Amsterdam node",
    keylink = BoardProxyKeylink.fromRaw("bproxy://${"A".repeat(86)}"),
)

@Preview(showBackground = true)
@Composable
private fun HomeDisconnectedPreview() {
    BoardVPNTheme {
        HomeScreen(
            state = HomeUiState(
                profiles = listOf(previewProfile),
                selectedProfileId = previewProfile.id,
            ),
            onAction = {},
            onProfileSelectorClick = {},
        )
    }
}

@Preview(showBackground = true)
@Composable
private fun HomeConnectedPreview() {
    BoardVPNTheme(themeMode = ThemeMode.Dark) {
        HomeScreen(
            state = HomeUiState(
                status = HomeConnectionStatus.Connected,
                profiles = listOf(previewProfile),
                selectedProfileId = previewProfile.id,
                statistics = VpnStatistics(
                    roundTripTimeMillis = 42,
                    uploadBytesPerSecond = 214_000,
                    downloadBytesPerSecond = 1_258_000,
                ),
                connectedDuration = 872.seconds,
            ),
            onAction = {},
            onProfileSelectorClick = {},
        )
    }
}
