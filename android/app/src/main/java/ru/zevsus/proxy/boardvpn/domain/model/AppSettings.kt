package ru.zevsus.proxy.boardvpn.domain.model

import kotlinx.serialization.Serializable

enum class ThemeMode {
    System,
    Light,
    Dark,
}

@Serializable
data class AppSettings(
    val themeMode: ThemeMode = ThemeMode.System,
    val dynamicColor: Boolean = true,
    val autoConnectOnLaunch: Boolean = false,
) {
    companion object {
        val Default = AppSettings()
    }
}
