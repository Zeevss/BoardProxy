package ru.zevsus.proxy.boardvpn.domain.repository

import kotlinx.coroutines.flow.Flow
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId

interface VpnProfileRepository {
    fun observeProfiles(): Flow<List<VpnProfile>>

    fun observeSelectedProfileId(): Flow<VpnProfileId?>

    suspend fun getProfile(id: VpnProfileId): VpnProfile?

    suspend fun saveProfile(profile: VpnProfile)

    suspend fun deleteProfile(id: VpnProfileId)

    suspend fun selectProfile(id: VpnProfileId?)
}
