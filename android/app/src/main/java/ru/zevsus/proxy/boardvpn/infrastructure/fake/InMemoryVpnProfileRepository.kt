package ru.zevsus.proxy.boardvpn.infrastructure.fake

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.repository.VpnProfileRepository

class InMemoryVpnProfileRepository(
    initialProfiles: List<VpnProfile> = emptyList(),
    initialSelectedProfileId: VpnProfileId? = null,
) : VpnProfileRepository {
    private val state = MutableStateFlow(
        State(
            profilesById = initialProfiles.associateBy(VpnProfile::id),
            selectedProfileId = initialSelectedProfileId,
        )
    )

    override fun observeProfiles(): Flow<List<VpnProfile>> = state
        .map { current -> current.profilesById.values.sortedBy { it.name.lowercase() } }
        .distinctUntilChanged()

    override fun observeSelectedProfileId(): Flow<VpnProfileId?> = state
        .map { current -> current.selectedProfileId?.takeIf(current.profilesById::containsKey) }
        .distinctUntilChanged()

    override suspend fun getProfile(id: VpnProfileId): VpnProfile? = state.value.profilesById[id]

    override suspend fun saveProfile(profile: VpnProfile) {
        state.update { it.copy(profilesById = it.profilesById + (profile.id to profile)) }
    }

    override suspend fun deleteProfile(id: VpnProfileId) {
        state.update { current ->
            current.copy(
                profilesById = current.profilesById - id,
                selectedProfileId = current.selectedProfileId.takeIf { it != id },
            )
        }
    }

    override suspend fun selectProfile(id: VpnProfileId?) {
        state.update { it.copy(selectedProfileId = id) }
    }

    private data class State(
        val profilesById: Map<VpnProfileId, VpnProfile>,
        val selectedProfileId: VpnProfileId?,
    )
}
