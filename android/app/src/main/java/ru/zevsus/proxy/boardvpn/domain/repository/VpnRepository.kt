package ru.zevsus.proxy.boardvpn.domain.repository

import kotlinx.coroutines.flow.Flow
import ru.zevsus.proxy.boardvpn.domain.model.VpnConnectResult
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState
import ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics

interface VpnRepository {
    fun observeSession(): Flow<VpnSessionState>

    fun observeStatistics(): Flow<VpnStatistics>

    suspend fun connect(profileId: VpnProfileId): VpnConnectResult

    suspend fun disconnect()

    suspend fun restart()
}
