package io.boardproxy.control.provisioning.domain.model

import java.net.URI
import java.time.Instant

private const val MAXIMUM_LANES = 32

data class Board(
    val id: String,
    val name: String,
    val hash: String,
    val hubSlide: String? = null,
    val apiBase: String? = null,
    val guestName: String? = null,
    val state: ResourceState,
    val maxLanes: Int,
    val version: Long,
    val updatedAt: Instant,
) {
    init {
        requireDomain(validId(id) && name.isNotBlank() && hash.isNotBlank() && version > 0, "invalid board")
        requireDomain(maxLanes in 1..MAXIMUM_LANES, "invalid board lane limit")
        apiBase?.takeIf(String::isNotBlank)?.let {
            val uri = runCatching { URI(it) }.getOrNull()
            requireDomain(
                uri != null && uri.isAbsolute && uri.host != null && uri.scheme in setOf("http", "https"),
                "invalid board API URL",
            )
        }
    }
}
