package ru.zevsus.proxy.boardvpn.infrastructure.persistence

import kotlinx.serialization.Serializable
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId

@Serializable
internal data class StoredProfiles(
    val profiles: List<VpnProfile> = emptyList(),
    val selectedProfileId: VpnProfileId? = null,
)
