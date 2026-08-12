package io.boardproxy.control.provisioning.domain.model

import java.time.Instant

data class Node(
    val id: String,
    val name: String,
    val state: ResourceState,
    val core: CoreSettings,
    val version: Long,
    val updatedAt: Instant,
) {
    init {
        requireDomain(validId(id) && name.isNotBlank() && version > 0, "invalid node identity")
        core.validate()
    }
}
