package ru.zevsus.proxy.boardvpn.domain.repository

import kotlinx.coroutines.flow.Flow
import ru.zevsus.proxy.boardvpn.domain.model.AppSettings
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode

interface AppSettingsRepository {
    fun observeSettings(): Flow<AppSettings>

    suspend fun setThemeMode(mode: ThemeMode)

    suspend fun setDynamicColor(enabled: Boolean)

    suspend fun setAutoConnectOnLaunch(enabled: Boolean)
}
