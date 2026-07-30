package ru.zevsus.proxy.boardvpn.ui.navigation

import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Settings
import androidx.annotation.StringRes
import kotlinx.serialization.Serializable
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.ui.components.BoardVpnShield

@Serializable
data object HomeDestination

@Serializable
data object ProfilesDestination

@Serializable
data object SettingsDestination

@Serializable
data object AppRoutingDestination

/** The three top-level tabs, in bottom bar order. */
enum class BoardVpnTab(
    val route: Any,
    @param:StringRes val labelRes: Int,
    val icon: ImageVector,
) {
    Home(HomeDestination, R.string.tab_home, BoardVpnShield),
    Profiles(ProfilesDestination, R.string.tab_profiles, Icons.AutoMirrored.Filled.List),
    Settings(SettingsDestination, R.string.tab_settings, Icons.Default.Settings),
}
