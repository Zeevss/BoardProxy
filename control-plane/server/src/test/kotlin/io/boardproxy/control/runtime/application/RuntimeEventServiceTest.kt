package io.boardproxy.control.runtime.application

import io.boardproxy.control.runtime.domain.RuntimeEvent
import io.boardproxy.control.runtime.domain.RuntimeEventPayload
import io.boardproxy.control.runtime.domain.RuntimeProjection
import io.boardproxy.control.runtime.domain.RuntimeResourceKind
import io.boardproxy.control.runtime.domain.RuntimeResourceOperation
import io.boardproxy.control.runtime.domain.RuntimeSnapshot
import io.boardproxy.control.runtime.domain.RuntimeUserSnapshot
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class RuntimeEventServiceTest {
    private val now = Instant.parse("2026-08-12T10:00:00Z")

    @Test
    fun `batch persists events and projection atomically then notifies`() {
        val store = MemoryStore()
        val notifications = mutableListOf<RuntimeProjection>()
        val service = RuntimeEventService(store, DirectTransactions, notifications::add)

        val result = service.store(batch("batch-1", event(1)))

        assertTrue(result.accepted)
        assertTrue(result.projectionChanged)
        assertEquals(1, store.projection.lastSequence)
        assertEquals(1, store.projection.version)
        assertEquals(1, notifications.size)
    }

    @Test
    fun `duplicate batch is acknowledged without applying or notifying twice`() {
        val store = MemoryStore()
        val notifications = mutableListOf<RuntimeProjection>()
        val service = RuntimeEventService(store, DirectTransactions, notifications::add)

        service.store(batch("batch-1", event(1)))
        val duplicate = service.store(batch("batch-1", event(1)))

        assertFalse(duplicate.accepted)
        assertFalse(duplicate.projectionChanged)
        assertEquals(1, store.projection.version)
        assertEquals(1, notifications.size)
    }

    @Test
    fun `same event replayed under a new batch remains harmless`() {
        val store = MemoryStore()
        val service = RuntimeEventService(store, DirectTransactions) { }

        service.store(batch("batch-1", event(1)))
        val replay = service.store(batch("batch-2", event(1)))

        assertTrue(replay.accepted)
        assertFalse(replay.projectionChanged)
        assertEquals(1, store.projection.version)
    }

    @Test
    fun `events inside one batch are projected by sequence`() {
        val store = MemoryStore()
        val service = RuntimeEventService(store, DirectTransactions) { }
        val second = RuntimeEvent(
            "event-2", "boot-1", 2, now.plusSeconds(2), 1,
            RuntimeEventPayload.ResourceChanged(
                RuntimeResourceKind.USER, RuntimeResourceOperation.DISABLED, "alice",
            ),
        )

        service.store(RuntimeEventBatch("node-1", "batch", listOf(second, event(1)), null, byteArrayOf(1)))

        assertEquals(2, store.projection.lastSequence)
        assertFalse(store.projection.users.getValue("alice").enabled)
        assertFalse(store.projection.gapDetected)
    }

    @Test
    fun `reset and snapshot replace stale projection before later events`() {
        val store = MemoryStore()
        val service = RuntimeEventService(store, DirectTransactions) { }
        service.store(batch("first", event(1)))
        val reset = RuntimeEvent(
            "reset", "boot-1", 0, now.plusSeconds(2), 2,
            RuntimeEventPayload.StreamReset("event_gap", 5, 8),
        )
        val snapshot = RuntimeSnapshot(
            "boot-1", 8, 2, now.plusSeconds(3),
            listOf(RuntimeUserSnapshot("bob", 0, 10, 20)), emptyList(),
        )

        service.store(RuntimeEventBatch("node-1", "reset-batch", listOf(reset), snapshot, byteArrayOf(2)))

        assertEquals(8, store.projection.lastSequence)
        assertEquals(setOf("bob"), store.projection.users.keys)
        assertFalse(store.projection.gapDetected)
    }

    private fun batch(batchId: String, event: RuntimeEvent) = RuntimeEventBatch(
        "node-1", batchId, listOf(event), null, batchId.toByteArray(),
    )

    private fun event(sequence: Long) = RuntimeEvent(
        "event-$sequence", "boot-1", sequence, now, 1,
        RuntimeEventPayload.ResourceChanged(
            RuntimeResourceKind.USER, RuntimeResourceOperation.ADDED, "alice",
        ),
    )

    private class MemoryStore : RuntimeEventStore {
        private val batches = mutableSetOf<String>()
        private val events = mutableSetOf<String>()
        var projection = RuntimeProjection("node-1")

        override fun claimBatch(batch: RuntimeEventBatch): Boolean = batches.add(batch.batchId)
        override fun appendEvent(nodeId: String, event: RuntimeEvent): Boolean = events.add(event.eventId)
        override fun lockProjection(nodeId: String): RuntimeProjection = projection
        override fun saveProjection(projection: RuntimeProjection) {
            this.projection = projection
        }
    }

    private object DirectTransactions : TransactionRunner {
        override fun <T> required(block: () -> T): T = block()
    }
}
