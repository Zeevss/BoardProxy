package ru.zevsus.proxy.boardvpn.domain.model

import kotlinx.serialization.Serializable

@Serializable
data class VpnProfile(
    val id: VpnProfileId,
    val name: String,
    val keylink: BoardProxyKeylink,
) {
    init {
        require(name.isNotBlank()) { "Profile name must not be blank" }
        require(name == name.trim()) { "Profile name must be trimmed" }
    }

    companion object {
        fun fromKeylink(keylink: BoardProxyKeylink): VpnProfile = VpnProfile(
            id = VpnProfileId("imported-${keylink.fingerprint()}"),
            name = keylink.label
                ?.replace(Regex("\\s+"), " ")
                ?.take(MAX_NAME_LENGTH)
                ?: "Imported profile",
            keylink = keylink,
        )

        private const val MAX_NAME_LENGTH = 64
    }
}
