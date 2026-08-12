package io.boardproxy.control.runtime.domain

import java.time.Instant

data class RuntimeUserState(
    val userTag: String,
    val enabled: Boolean,
    val activeSessions: Long = 0,
    val rxBytesAtSnapshot: Long = 0,
    val txBytesAtSnapshot: Long = 0,
)

data class RuntimeBoardState(
    val boardTag: String,
    val enabled: Boolean,
    val state: String = "unknown",
    val error: String = "",
)

data class RuntimeSessionState(
    val bundleId: String,
    val userTag: String,
    val boardTag: String,
    val openedAt: Instant,
)

data class RuntimeUserSnapshot(
    val userTag: String,
    val activeSessions: Long,
    val rxBytes: Long,
    val txBytes: Long,
)

data class RuntimeBoardSnapshot(
    val boardTag: String,
    val state: String,
    val error: String,
)

data class RuntimeSnapshot(
    val coreBootId: String,
    val latestSequence: Long,
    val runtimeRevision: Long,
    val capturedAt: Instant,
    val users: List<RuntimeUserSnapshot>,
    val boards: List<RuntimeBoardSnapshot>,
)

data class RuntimeProjection(
    val nodeId: String,
    val coreBootId: String? = null,
    val lastSequence: Long = 0,
    val runtimeRevision: Long = 0,
    val gapDetected: Boolean = false,
    val lastEventAt: Instant? = null,
    val capturedAt: Instant? = null,
    val users: Map<String, RuntimeUserState> = emptyMap(),
    val boards: Map<String, RuntimeBoardState> = emptyMap(),
    val sessions: Map<String, RuntimeSessionState> = emptyMap(),
    val sessionDetailsComplete: Boolean = true,
    val version: Long = 0,
) {
    fun apply(event: RuntimeEvent): RuntimeProjection {
        if (event.payload is RuntimeEventPayload.StreamReset) return applyReset(event)
        val current = if (coreBootId == event.coreBootId) this else emptyForBoot(event.coreBootId)
        if (event.sequence <= current.lastSequence) return current
        if (current.gapDetected || event.sequence != current.lastSequence + 1) {
            return current.copy(
                runtimeRevision = maxOf(current.runtimeRevision, event.runtimeRevision),
                gapDetected = true,
                lastEventAt = event.occurredAt,
            )
        }
        return current.applyPayload(event).copy(
            lastSequence = event.sequence,
            runtimeRevision = maxOf(current.runtimeRevision, event.runtimeRevision),
            lastEventAt = event.occurredAt,
        )
    }

    fun replace(snapshot: RuntimeSnapshot): RuntimeProjection {
        if (coreBootId == snapshot.coreBootId && !gapDetected && snapshot.latestSequence < lastSequence) return this
        val snapshotUsers = snapshot.users.associate { value ->
            value.userTag to RuntimeUserState(
                value.userTag, enabled = true, value.activeSessions, value.rxBytes, value.txBytes,
            )
        }
        val allSessionsKnown = snapshotUsers.values.all { it.activeSessions == 0L }
        return copy(
            coreBootId = snapshot.coreBootId,
            lastSequence = snapshot.latestSequence,
            runtimeRevision = snapshot.runtimeRevision,
            gapDetected = false,
            lastEventAt = snapshot.capturedAt,
            capturedAt = snapshot.capturedAt,
            users = snapshotUsers,
            boards = snapshot.boards.associate { value ->
                value.boardTag to RuntimeBoardState(value.boardTag, enabled = true, value.state, value.error)
            },
            sessions = emptyMap(),
            sessionDetailsComplete = allSessionsKnown,
        )
    }

    fun nextVersion(): RuntimeProjection = copy(version = version + 1)

    private fun applyReset(event: RuntimeEvent): RuntimeProjection {
        val current = if (coreBootId == event.coreBootId) this else emptyForBoot(event.coreBootId)
        return current.copy(
            runtimeRevision = maxOf(current.runtimeRevision, event.runtimeRevision),
            gapDetected = true,
            lastEventAt = event.occurredAt,
            sessionDetailsComplete = false,
        )
    }

    private fun applyPayload(event: RuntimeEvent): RuntimeProjection = when (val value = event.payload) {
        is RuntimeEventPayload.ResourceChanged -> applyResource(value)
        is RuntimeEventPayload.BoardStateChanged -> copy(
            boards = boards + (value.boardTag to RuntimeBoardState(
                boardTag = value.boardTag,
                enabled = boards[value.boardTag]?.enabled ?: true,
                state = value.state,
                error = value.error,
            )),
        )
        is RuntimeEventPayload.ClientSessionOpened -> openSession(event, value)
        is RuntimeEventPayload.ClientSessionClosed -> closeSession(value)
        is RuntimeEventPayload.StreamReset -> error("stream reset is handled before payload dispatch")
    }

    private fun applyResource(change: RuntimeEventPayload.ResourceChanged): RuntimeProjection = when (change.kind) {
        RuntimeResourceKind.USER -> applyUserResource(change)
        RuntimeResourceKind.BOARD -> applyBoardResource(change)
    }

    private fun applyUserResource(change: RuntimeEventPayload.ResourceChanged): RuntimeProjection {
        if (change.operation == RuntimeResourceOperation.REMOVED) {
            val remainingSessions = sessions.filterValues { it.userTag != change.tag }
            return copy(
                users = users - change.tag,
                sessions = remainingSessions,
                sessionDetailsComplete = completeWhenIdle(users - change.tag, sessionDetailsComplete),
            )
        }
        val current = users[change.tag] ?: RuntimeUserState(change.tag, enabled = true)
        val enabled = when (change.operation) {
            RuntimeResourceOperation.DISABLED -> false
            RuntimeResourceOperation.ADDED, RuntimeResourceOperation.ENABLED -> true
            RuntimeResourceOperation.UPDATED -> current.enabled
            RuntimeResourceOperation.REMOVED -> error("removed is handled above")
        }
        return copy(users = users + (change.tag to current.copy(enabled = enabled)))
    }

    private fun applyBoardResource(change: RuntimeEventPayload.ResourceChanged): RuntimeProjection {
        if (change.operation == RuntimeResourceOperation.REMOVED) {
            val removed = sessions.values.filter { it.boardTag == change.tag }
            val remainingUsers = removed.fold(users) { state, session -> decrement(state, session.userTag) }
            val remainingSessions = sessions.filterValues { it.boardTag != change.tag }
            return copy(
                users = remainingUsers,
                boards = boards - change.tag,
                sessions = remainingSessions,
                sessionDetailsComplete = completeWhenIdle(remainingUsers, sessionDetailsComplete),
            )
        }
        val current = boards[change.tag] ?: RuntimeBoardState(change.tag, enabled = true)
        val enabled = when (change.operation) {
            RuntimeResourceOperation.DISABLED -> false
            RuntimeResourceOperation.ADDED, RuntimeResourceOperation.ENABLED -> true
            RuntimeResourceOperation.UPDATED -> current.enabled
            RuntimeResourceOperation.REMOVED -> error("removed is handled above")
        }
        return copy(boards = boards + (change.tag to current.copy(enabled = enabled)))
    }

    private fun openSession(
        event: RuntimeEvent,
        opened: RuntimeEventPayload.ClientSessionOpened,
    ): RuntimeProjection {
        if (sessions.containsKey(opened.bundleId)) return this
        val current = users[opened.userTag] ?: RuntimeUserState(opened.userTag, enabled = true)
        return copy(
            users = users + (opened.userTag to current.copy(activeSessions = current.activeSessions + 1)),
            sessions = sessions + (opened.bundleId to RuntimeSessionState(
                opened.bundleId, opened.userTag, opened.boardTag, event.occurredAt,
            )),
        )
    }

    private fun closeSession(closed: RuntimeEventPayload.ClientSessionClosed): RuntimeProjection {
        val known = sessions[closed.bundleId]
        if (known == null && sessionDetailsComplete) return this
        val userTag = known?.userTag ?: closed.userTag
        val remainingUsers = decrement(users, userTag)
        return copy(
            users = remainingUsers,
            sessions = sessions - closed.bundleId,
            sessionDetailsComplete = completeWhenIdle(remainingUsers, sessionDetailsComplete),
        )
    }

    private fun emptyForBoot(bootId: String) = RuntimeProjection(nodeId = nodeId, coreBootId = bootId, version = version)

    private fun decrement(state: Map<String, RuntimeUserState>, userTag: String): Map<String, RuntimeUserState> {
        val current = state[userTag] ?: return state
        return state + (userTag to current.copy(activeSessions = maxOf(0, current.activeSessions - 1)))
    }

    private fun completeWhenIdle(state: Map<String, RuntimeUserState>, current: Boolean): Boolean =
        current || state.values.all { it.activeSessions == 0L }
}
