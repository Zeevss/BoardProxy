package io.boardproxy.control.provisioning.domain.model

import java.time.Instant

data class ConfigRevision(
    val nodeId: String,
    val revision: Long,
    val previousRevision: Long,
    val catalogVersion: Long,
    val configToml: ByteArray,
    val configSha256: String,
    val cause: String,
    val createdAt: Instant,
) {
    override fun equals(other: Any?): Boolean = other is ConfigRevision &&
        nodeId == other.nodeId && revision == other.revision && configSha256 == other.configSha256

    override fun hashCode(): Int = 31 * nodeId.hashCode() + revision.hashCode()
}
