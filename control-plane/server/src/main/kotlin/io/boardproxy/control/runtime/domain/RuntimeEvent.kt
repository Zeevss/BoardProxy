package io.boardproxy.control.runtime.domain

import java.time.Instant

enum class RuntimeResourceKind { USER, BOARD }

enum class RuntimeResourceOperation { ADDED, UPDATED, ENABLED, DISABLED, REMOVED }

sealed interface RuntimeEventPayload {
    data class ResourceChanged(
        val kind: RuntimeResourceKind,
        val operation: RuntimeResourceOperation,
        val tag: String,
    ) : RuntimeEventPayload

    data class BoardStateChanged(
        val boardTag: String,
        val previousState: String,
        val state: String,
        val error: String,
    ) : RuntimeEventPayload

    data class ClientSessionOpened(
        val userTag: String,
        val boardTag: String,
        val bundleId: String,
    ) : RuntimeEventPayload

    data class ClientSessionClosed(
        val userTag: String,
        val boardTag: String,
        val bundleId: String,
        val rxBytes: Long,
        val txBytes: Long,
        val reason: String,
    ) : RuntimeEventPayload

    data class StreamReset(
        val reason: String,
        val oldestAvailableSequence: Long,
        val latestSequence: Long,
    ) : RuntimeEventPayload
}

data class RuntimeEvent(
    val eventId: String,
    val coreBootId: String,
    val sequence: Long,
    val occurredAt: Instant,
    val runtimeRevision: Long,
    val payload: RuntimeEventPayload,
)

fun RuntimeEvent.type(): String = when (payload) {
    is RuntimeEventPayload.ResourceChanged -> "resource.changed"
    is RuntimeEventPayload.BoardStateChanged -> "board.state.changed"
    is RuntimeEventPayload.ClientSessionOpened -> "client.session.opened"
    is RuntimeEventPayload.ClientSessionClosed -> "client.session.closed"
    is RuntimeEventPayload.StreamReset -> "stream.reset"
}
