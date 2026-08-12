package io.boardproxy.control.runtime.api.rest

import io.boardproxy.control.runtime.application.RuntimeEventView
import io.boardproxy.control.runtime.domain.RuntimeProjection
import java.time.Instant

data class RuntimeProjectionResponse(
    val nodeId: String,
    val coreBootId: String?,
    val lastSequence: Long,
    val runtimeRevision: Long,
    val gapDetected: Boolean,
    val lastEventAt: Instant?,
    val capturedAt: Instant?,
    val sessionDetailsComplete: Boolean,
    val users: List<RuntimeUserResponse>,
    val boards: List<RuntimeBoardResponse>,
    val sessions: List<RuntimeSessionResponse>,
    val version: Long,
)

data class RuntimeUserResponse(
    val userTag: String,
    val enabled: Boolean,
    val activeSessions: Long,
    val rxBytesAtSnapshot: Long,
    val txBytesAtSnapshot: Long,
)

data class RuntimeBoardResponse(
    val boardTag: String,
    val enabled: Boolean,
    val state: String,
    val error: String,
)

data class RuntimeSessionResponse(
    val bundleId: String,
    val userTag: String,
    val boardTag: String,
    val openedAt: Instant,
)

data class RuntimeEventResponse(
    val eventId: String,
    val coreBootId: String,
    val sequence: Long,
    val runtimeRevision: Long,
    val type: String,
    val payload: Map<String, Any?>,
    val occurredAt: Instant,
    val receivedAt: Instant,
)

internal fun RuntimeProjection.toResponse() = RuntimeProjectionResponse(
    nodeId = nodeId,
    coreBootId = coreBootId,
    lastSequence = lastSequence,
    runtimeRevision = runtimeRevision,
    gapDetected = gapDetected,
    lastEventAt = lastEventAt,
    capturedAt = capturedAt,
    sessionDetailsComplete = sessionDetailsComplete,
    users = users.values.sortedBy { it.userTag }.map {
        RuntimeUserResponse(
            it.userTag, it.enabled, it.activeSessions, it.rxBytesAtSnapshot, it.txBytesAtSnapshot,
        )
    },
    boards = boards.values.sortedBy { it.boardTag }.map {
        RuntimeBoardResponse(it.boardTag, it.enabled, it.state, it.error)
    },
    sessions = sessions.values.sortedBy { it.bundleId }.map {
        RuntimeSessionResponse(it.bundleId, it.userTag, it.boardTag, it.openedAt)
    },
    version = version,
)

internal fun RuntimeEventView.toResponse() = RuntimeEventResponse(
    eventId, coreBootId, sequence, runtimeRevision, type, payload, occurredAt, receivedAt,
)
