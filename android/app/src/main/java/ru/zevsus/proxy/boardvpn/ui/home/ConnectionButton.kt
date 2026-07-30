package ru.zevsus.proxy.boardvpn.ui.home

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import ru.zevsus.proxy.boardvpn.ui.components.BoardVpnShield
import ru.zevsus.proxy.boardvpn.ui.theme.LocalConnectionColors

/**
 * The single control of the home screen: a shield that fills in as the session
 * comes up and pulses while it is negotiating.
 */
@Composable
fun ConnectionButton(
    status: HomeConnectionStatus,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    size: Dp = 200.dp,
) {
    val connectionColors = LocalConnectionColors.current
    val colorScheme = MaterialTheme.colorScheme

    val accent = when (status) {
        HomeConnectionStatus.Disconnected -> colorScheme.primary
        HomeConnectionStatus.Connecting -> colorScheme.primary
        HomeConnectionStatus.Connected -> connectionColors.connected
        HomeConnectionStatus.Reconnecting -> connectionColors.reconnecting
        HomeConnectionStatus.Disconnecting -> colorScheme.outline
    }
    val filled = status == HomeConnectionStatus.Connected ||
        status == HomeConnectionStatus.Reconnecting

    val containerColor by animateColorAsState(
        targetValue = if (filled) accent else colorScheme.surfaceVariant,
        animationSpec = tween(durationMillis = 450),
        label = "containerColor",
    )
    val shieldColor by animateColorAsState(
        targetValue = when {
            !enabled -> colorScheme.outline
            filled -> colorScheme.surface
            else -> accent
        },
        animationSpec = tween(durationMillis = 450),
        label = "shieldColor",
    )

    val interactionSource = remember { MutableInteractionSource() }
    val pressed by interactionSource.collectIsPressedAsState()
    val pressScale by animateFloatAsState(
        targetValue = if (pressed) 0.94f else 1f,
        animationSpec = spring(dampingRatio = Spring.DampingRatioMediumBouncy),
        label = "pressScale",
    )

    Box(
        modifier = modifier.size(size),
        contentAlignment = Alignment.Center,
    ) {
        if (status == HomeConnectionStatus.Connecting ||
            status == HomeConnectionStatus.Reconnecting
        ) {
            PulseRing(color = accent, size = size)
        }

        Box(
            modifier = Modifier
                .scale(pressScale)
                .size(size * 0.82f)
                .background(containerColor, CircleShape)
                .border(width = 1.dp, color = accent.copy(alpha = 0.35f), shape = CircleShape)
                .toggleable(
                    value = filled,
                    enabled = enabled,
                    role = Role.Switch,
                    interactionSource = interactionSource,
                    indication = null,
                    onValueChange = { onClick() },
                ),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = BoardVpnShield,
                contentDescription = null,
                tint = shieldColor,
                modifier = Modifier.size(size * 0.38f),
            )
        }
    }
}

@Composable
private fun PulseRing(color: Color, size: Dp) {
    val transition = rememberInfiniteTransition(label = "pulse")
    val scale by transition.animateFloat(
        initialValue = 0.84f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 1_400, easing = FastOutSlowInEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "pulseScale",
    )
    val alpha by transition.animateFloat(
        initialValue = 0.45f,
        targetValue = 0.05f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 1_400, easing = FastOutSlowInEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "pulseAlpha",
    )

    Box(
        modifier = Modifier
            .size(size)
            .scale(scale)
            .background(color.copy(alpha = alpha), CircleShape)
    )
}
