package io.boardproxy.control.delivery.api.grpc

import bproxy.node.v1.Node
import io.boardproxy.control.runtime.application.RuntimeEventBatch
import io.boardproxy.control.runtime.domain.RuntimeBoardSnapshot
import io.boardproxy.control.runtime.domain.RuntimeEvent
import io.boardproxy.control.runtime.domain.RuntimeEventPayload
import io.boardproxy.control.runtime.domain.RuntimeResourceKind
import io.boardproxy.control.runtime.domain.RuntimeResourceOperation
import io.boardproxy.control.runtime.domain.RuntimeSnapshot
import io.boardproxy.control.runtime.domain.RuntimeUserSnapshot
import io.grpc.Status
import java.time.Instant

internal fun Node.RuntimeEventBatch.toDomain(nodeId: String): RuntimeEventBatch {
    valid(batchId.isNotBlank(), "runtime event batch id is required")
    valid(eventsCount > 0 || hasSnapshot(), "runtime event batch payload is required")
    val mappedEvents = eventsList.map(Node.CoreRuntimeEvent::toDomain)
    val mappedSnapshot = if (hasSnapshot()) snapshot.toDomain() else null
    return RuntimeEventBatch(nodeId, batchId, mappedEvents, mappedSnapshot, toByteArray())
}

private fun Node.CoreRuntimeEvent.toDomain(): RuntimeEvent {
    valid(eventId.isNotBlank(), "runtime event id is required")
    valid(coreBootId.isNotBlank(), "runtime event core boot id is required")
    valid(hasOccurredAt(), "runtime event occurred_at is required")
    valid(sequence >= 0, "runtime event sequence exceeds supported range")
    valid(runtimeRevision >= 0, "runtime revision exceeds supported range")
    if (payloadCase != Node.CoreRuntimeEvent.PayloadCase.STREAM_RESET) {
        valid(sequence > 0, "non-reset runtime event sequence must be positive")
    } else {
        valid(sequence == 0L, "stream reset sequence must be zero")
    }
    return RuntimeEvent(
        eventId = eventId,
        coreBootId = coreBootId,
        sequence = sequence,
        occurredAt = Instant.ofEpochSecond(occurredAt.seconds, occurredAt.nanos.toLong()),
        runtimeRevision = runtimeRevision,
        payload = when (payloadCase) {
            Node.CoreRuntimeEvent.PayloadCase.RESOURCE_CHANGED -> resourceChanged.toDomain()
            Node.CoreRuntimeEvent.PayloadCase.BOARD_STATE_CHANGED -> RuntimeEventPayload.BoardStateChanged(
                boardTag = boardStateChanged.boardTag.required("board tag"),
                previousState = boardStateChanged.previousState,
                state = boardStateChanged.state.required("board state"),
                error = boardStateChanged.error,
            )
            Node.CoreRuntimeEvent.PayloadCase.CLIENT_SESSION_OPENED -> RuntimeEventPayload.ClientSessionOpened(
                userTag = clientSessionOpened.userTag.required("session user tag"),
                boardTag = clientSessionOpened.boardTag.required("session board tag"),
                bundleId = clientSessionOpened.bundleId.required("session bundle id"),
            )
            Node.CoreRuntimeEvent.PayloadCase.CLIENT_SESSION_CLOSED -> RuntimeEventPayload.ClientSessionClosed(
                userTag = clientSessionClosed.userTag.required("session user tag"),
                boardTag = clientSessionClosed.boardTag.required("session board tag"),
                bundleId = clientSessionClosed.bundleId.required("session bundle id"),
                rxBytes = clientSessionClosed.rxBytes.nonNegative("session rx_bytes"),
                txBytes = clientSessionClosed.txBytes.nonNegative("session tx_bytes"),
                reason = clientSessionClosed.reason,
            )
            Node.CoreRuntimeEvent.PayloadCase.STREAM_RESET -> RuntimeEventPayload.StreamReset(
                reason = streamReset.reason.required("stream reset reason"),
                oldestAvailableSequence = streamReset.oldestAvailableSequence.nonNegative(
                    "oldest available sequence",
                ),
                latestSequence = streamReset.latestSequence.nonNegative("latest sequence"),
            )
            Node.CoreRuntimeEvent.PayloadCase.PAYLOAD_NOT_SET -> invalid("runtime event payload is required")
        },
    )
}

private fun Node.ResourceChanged.toDomain(): RuntimeEventPayload.ResourceChanged {
    val mappedKind = when (kind) {
        Node.ResourceKind.RESOURCE_KIND_USER -> RuntimeResourceKind.USER
        Node.ResourceKind.RESOURCE_KIND_BOARD -> RuntimeResourceKind.BOARD
        Node.ResourceKind.RESOURCE_KIND_UNSPECIFIED, Node.ResourceKind.UNRECOGNIZED ->
            invalid("runtime resource kind is required")
    }
    val mappedOperation = when (operation) {
        Node.ResourceOperation.RESOURCE_OPERATION_ADDED -> RuntimeResourceOperation.ADDED
        Node.ResourceOperation.RESOURCE_OPERATION_UPDATED -> RuntimeResourceOperation.UPDATED
        Node.ResourceOperation.RESOURCE_OPERATION_ENABLED -> RuntimeResourceOperation.ENABLED
        Node.ResourceOperation.RESOURCE_OPERATION_DISABLED -> RuntimeResourceOperation.DISABLED
        Node.ResourceOperation.RESOURCE_OPERATION_REMOVED -> RuntimeResourceOperation.REMOVED
        Node.ResourceOperation.RESOURCE_OPERATION_UNSPECIFIED, Node.ResourceOperation.UNRECOGNIZED ->
            invalid("runtime resource operation is required")
    }
    return RuntimeEventPayload.ResourceChanged(mappedKind, mappedOperation, tag.required("runtime resource tag"))
}

private fun Node.RuntimeSnapshot.toDomain(): RuntimeSnapshot {
    valid(coreBootId.isNotBlank(), "runtime snapshot core boot id is required")
    valid(hasCapturedAt(), "runtime snapshot captured_at is required")
    val mappedUsers = usersList.map { user ->
        RuntimeUserSnapshot(
            userTag = user.userTag.required("runtime snapshot user tag"),
            activeSessions = user.activeSessions.nonNegative("active sessions"),
            rxBytes = user.rxBytes.nonNegative("runtime snapshot rx_bytes"),
            txBytes = user.txBytes.nonNegative("runtime snapshot tx_bytes"),
        )
    }
    val mappedBoards = boardsList.map { board ->
        RuntimeBoardSnapshot(
            boardTag = board.boardTag.required("runtime snapshot board tag"),
            state = board.state.required("runtime snapshot board state"),
            error = board.error,
        )
    }
    valid(mappedUsers.map { it.userTag }.distinct().size == mappedUsers.size, "runtime snapshot has duplicate users")
    valid(mappedBoards.map { it.boardTag }.distinct().size == mappedBoards.size, "runtime snapshot has duplicate boards")
    return RuntimeSnapshot(
        coreBootId = coreBootId,
        latestSequence = latestSequence.nonNegative("runtime snapshot sequence"),
        runtimeRevision = runtimeRevision.nonNegative("runtime snapshot revision"),
        capturedAt = Instant.ofEpochSecond(capturedAt.seconds, capturedAt.nanos.toLong()),
        users = mappedUsers,
        boards = mappedBoards,
    )
}

private fun Long.nonNegative(field: String): Long {
    valid(this >= 0, "$field exceeds supported range")
    return this
}

private fun String.required(field: String): String {
    valid(isNotBlank(), "$field is required")
    return this
}

private fun valid(condition: Boolean, message: String) {
    if (!condition) invalid(message)
}

private fun invalid(message: String): Nothing =
    throw Status.INVALID_ARGUMENT.withDescription(message).asRuntimeException()
