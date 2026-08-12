package io.boardproxy.control.runtime.domain

import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class RuntimeProjectionTest {
    private val now = Instant.parse("2026-08-12T10:00:00Z")

    @Test
    fun `contiguous events build users boards and active sessions`() {
        val projection = RuntimeProjection("node-1")
            .apply(event(1, RuntimeEventPayload.ResourceChanged(
                RuntimeResourceKind.USER, RuntimeResourceOperation.ADDED, "alice",
            )))
            .apply(event(2, RuntimeEventPayload.ResourceChanged(
                RuntimeResourceKind.BOARD, RuntimeResourceOperation.ADDED, "main",
            )))
            .apply(event(3, RuntimeEventPayload.ClientSessionOpened("alice", "main", "bundle-1")))
            .apply(event(4, RuntimeEventPayload.BoardStateChanged("main", "connecting", "ready", "")))

        assertEquals(4, projection.lastSequence)
        assertEquals(1, projection.users.getValue("alice").activeSessions)
        assertEquals("ready", projection.boards.getValue("main").state)
        assertEquals("alice", projection.sessions.getValue("bundle-1").userTag)
        assertFalse(projection.gapDetected)
        assertTrue(projection.sessionDetailsComplete)
    }

    @Test
    fun `gap freezes incremental projection until authoritative snapshot`() {
        val gap = RuntimeProjection("node-1")
            .apply(event(1, RuntimeEventPayload.ResourceChanged(
                RuntimeResourceKind.USER, RuntimeResourceOperation.ADDED, "old",
            )))
            .apply(event(3, RuntimeEventPayload.ResourceChanged(
                RuntimeResourceKind.USER, RuntimeResourceOperation.ADDED, "must-not-appear",
            )))

        assertTrue(gap.gapDetected)
        assertEquals(1, gap.lastSequence)
        assertFalse(gap.users.containsKey("must-not-appear"))

        val replaced = gap.replace(
            RuntimeSnapshot(
                "boot-1", latestSequence = 5, runtimeRevision = 8, capturedAt = now.plusSeconds(5),
                users = listOf(RuntimeUserSnapshot("alice", 2, 10, 20)),
                boards = listOf(RuntimeBoardSnapshot("main", "ready", "")),
            ),
        )

        assertFalse(replaced.gapDetected)
        assertEquals(5, replaced.lastSequence)
        assertEquals(setOf("alice"), replaced.users.keys)
        assertEquals(2, replaced.users.getValue("alice").activeSessions)
        assertFalse(replaced.sessionDetailsComplete)
        assertTrue(replaced.sessions.isEmpty())
    }

    @Test
    fun `session details become complete after all snapshot sessions close`() {
        val snapshot = RuntimeProjection("node-1").replace(
            RuntimeSnapshot(
                "boot-1", 5, 8, now,
                listOf(RuntimeUserSnapshot("alice", 1, 100, 200)), emptyList(),
            ),
        )
        val closed = snapshot.apply(
            event(
                6,
                RuntimeEventPayload.ClientSessionClosed("alice", "main", "unknown-bundle", 1, 2, "closed"),
            ),
        )

        assertEquals(0, closed.users.getValue("alice").activeSessions)
        assertTrue(closed.sessionDetailsComplete)
    }

    @Test
    fun `reset with zero sequence marks projection stale without moving cursor`() {
        val projection = RuntimeProjection("node-1")
            .apply(event(1, RuntimeEventPayload.ResourceChanged(
                RuntimeResourceKind.USER, RuntimeResourceOperation.ADDED, "alice",
            )))
            .apply(event(0, RuntimeEventPayload.StreamReset("event_gap", 10, 20)))

        assertEquals(1, projection.lastSequence)
        assertTrue(projection.gapDetected)
        assertFalse(projection.sessionDetailsComplete)
    }

    private fun event(sequence: Long, payload: RuntimeEventPayload) = RuntimeEvent(
        eventId = "boot-1:$sequence:${payload::class.simpleName}",
        coreBootId = "boot-1",
        sequence = sequence,
        occurredAt = now.plusSeconds(sequence),
        runtimeRevision = 7,
        payload = payload,
    )
}
