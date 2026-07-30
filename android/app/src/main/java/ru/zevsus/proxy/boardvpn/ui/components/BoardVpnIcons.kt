package ru.zevsus.proxy.boardvpn.ui.components

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathFillType
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.path
import androidx.compose.ui.unit.dp

/**
 * Hand-drawn shield used as the app symbol: the connect button, the navigation
 * icon and the empty profile state all share it, so no icon dependency is
 * needed.
 */
val BoardVpnShield: ImageVector by lazy {
    ImageVector.Builder(
        name = "BoardVpnShield",
        defaultWidth = 24.dp,
        defaultHeight = 24.dp,
        viewportWidth = 24f,
        viewportHeight = 24f,
    ).apply {
        path(fill = SolidColor(Color.White)) {
            moveTo(12f, 2f)
            lineTo(4f, 5.4f)
            verticalLineTo(11.2f)
            curveTo(4f, 16.1f, 7.4f, 20.6f, 12f, 22f)
            curveTo(16.6f, 20.6f, 20f, 16.1f, 20f, 11.2f)
            verticalLineTo(5.4f)
            close()
        }
    }.build()
}

/** Outlined variant, used where the filled shield would be too heavy. */
val BoardVpnShieldOutline: ImageVector by lazy {
    ImageVector.Builder(
        name = "BoardVpnShieldOutline",
        defaultWidth = 24.dp,
        defaultHeight = 24.dp,
        viewportWidth = 24f,
        viewportHeight = 24f,
    ).apply {
        path(fill = SolidColor(Color.White), pathFillType = PathFillType.EvenOdd) {
            moveTo(12f, 2f)
            lineTo(4f, 5.4f)
            verticalLineTo(11.2f)
            curveTo(4f, 16.1f, 7.4f, 20.6f, 12f, 22f)
            curveTo(16.6f, 20.6f, 20f, 16.1f, 20f, 11.2f)
            verticalLineTo(5.4f)
            close()
            moveTo(12f, 4.2f)
            lineTo(18f, 6.7f)
            verticalLineTo(11.2f)
            curveTo(18f, 15f, 15.5f, 18.6f, 12f, 19.9f)
            curveTo(8.5f, 18.6f, 6f, 15f, 6f, 11.2f)
            verticalLineTo(6.7f)
            close()
        }
    }.build()
}
