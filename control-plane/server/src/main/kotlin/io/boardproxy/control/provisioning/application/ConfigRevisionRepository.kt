package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.ConfigRevision
import java.time.Instant

interface ConfigRevisionRepository {
    fun append(
        nodeId: String,
        catalogVersion: Long,
        configToml: ByteArray,
        cause: String,
        createdAt: Instant,
    ): ConfigRevision

    fun latest(nodeId: String): ConfigRevision?
}
