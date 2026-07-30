package ru.zevsus.proxy.boardvpn.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode

private val LightColors = lightColorScheme(
    primary = IndigoLight,
    onPrimary = Color.White,
    primaryContainer = IndigoContainerLight,
    onPrimaryContainer = Color(0xFF00105C),
    secondary = TealLight,
    onSecondary = Color.White,
    background = SurfaceLight,
    onBackground = OnSurfaceLight,
    surface = SurfaceLight,
    onSurface = OnSurfaceLight,
    surfaceVariant = SurfaceContainerLight,
    onSurfaceVariant = OnSurfaceVariantLight,
    surfaceContainer = SurfaceContainerLight,
    outline = OutlineLight,
    outlineVariant = OutlineLight,
    error = ErrorLight,
)

private val DarkColors = darkColorScheme(
    primary = IndigoDark,
    onPrimary = Color(0xFF00105C),
    primaryContainer = IndigoContainerDark,
    onPrimaryContainer = IndigoContainerLight,
    secondary = TealDark,
    onSecondary = Color(0xFF00382F),
    background = SurfaceDark,
    onBackground = OnSurfaceDark,
    surface = SurfaceDark,
    onSurface = OnSurfaceDark,
    surfaceVariant = SurfaceContainerDark,
    onSurfaceVariant = OnSurfaceVariantDark,
    surfaceContainer = SurfaceContainerDark,
    outline = OutlineDark,
    outlineVariant = OutlineDark,
    error = ErrorDark,
)

/** Colors that carry connection meaning and therefore stay outside the M3 roles. */
@Immutable
data class ConnectionColors(
    val connected: Color,
    val reconnecting: Color,
)

val LocalConnectionColors = staticCompositionLocalOf {
    ConnectionColors(connected = ConnectedLight, reconnecting = ReconnectingLight)
}

@Composable
fun BoardVPNTheme(
    themeMode: ThemeMode = ThemeMode.System,
    content: @Composable () -> Unit,
) {
    val darkTheme = when (themeMode) {
        ThemeMode.System -> isSystemInDarkTheme()
        ThemeMode.Light -> false
        ThemeMode.Dark -> true
    }

    val colorScheme = if (darkTheme) DarkColors else LightColors

    val connectionColors = if (darkTheme) {
        ConnectionColors(connected = ConnectedDark, reconnecting = ReconnectingDark)
    } else {
        ConnectionColors(connected = ConnectedLight, reconnecting = ReconnectingLight)
    }

    CompositionLocalProvider(LocalConnectionColors provides connectionColors) {
        MaterialTheme(
            colorScheme = colorScheme,
            typography = Typography,
            content = content,
        )
    }
}
