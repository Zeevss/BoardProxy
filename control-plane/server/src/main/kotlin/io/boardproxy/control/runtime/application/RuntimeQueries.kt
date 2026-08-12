package io.boardproxy.control.runtime.application

import io.boardproxy.control.runtime.domain.RuntimeProjection
import java.time.Instant

data class RuntimeEventView(
    val eventId: String,
    val coreBootId: String,
    val sequence: Long,
    val runtimeRevision: Long,
    val type: String,
    val payload: Map<String, Any?>,
    val occurredAt: Instant,
    val receivedAt: Instant,
)

interface RuntimeQueries {
    fun projection(nodeId: String): RuntimeProjection?

    fun events(
        nodeId: String,
        coreBootId: String?,
        afterSequence: Long?,
        limit: Int,
    ): List<RuntimeEventView>
}
