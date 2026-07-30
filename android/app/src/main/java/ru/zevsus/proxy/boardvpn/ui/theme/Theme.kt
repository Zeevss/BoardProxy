package ru.zevsus.proxy.boardvpn.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
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

/** True when the device can honour the Material You setting. */
val dynamicColorAvailable: Boolean
    get() = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S

@Composable
fun BoardVPNTheme(
    themeMode: ThemeMode = ThemeMode.System,
    dynamicColor: Boolean = true,
    content: @Composable () -> Unit,
) {
    val darkTheme = when (themeMode) {
        ThemeMode.System -> isSystemInDarkTheme()
        ThemeMode.Light -> false
        ThemeMode.Dark -> true
    }

    val colorScheme = when {
        dynamicColor && dynamicColorAvailable -> {
            val context = LocalContext.current
            if (darkTheme) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        }
        darkTheme -> DarkColors
        else -> LightColors
    }

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
