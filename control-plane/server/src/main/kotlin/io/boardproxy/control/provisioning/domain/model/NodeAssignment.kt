package io.boardproxy.control.provisioning.domain.model

import java.time.Instant

data class AssignedUser(val userId: String, val boardIds: List<String>)

data class NodeAssignment(
    val nodeId: String,
    val boardIds: List<String>,
    val users: List<AssignedUser>,
    val version: Long,
    val updatedAt: Instant,
) {
    init {
        requireDomain(validId(nodeId) && version > 0, "invalid assignment identity")
    }
}
