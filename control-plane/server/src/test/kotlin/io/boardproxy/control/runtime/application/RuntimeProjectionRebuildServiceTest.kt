package io.boardproxy.control.runtime.application

import io.boardproxy.control.runtime.domain.RuntimeEvent
import io.boardproxy.control.runtime.domain.RuntimeEventPayload
import io.boardproxy.control.runtime.domain.RuntimeProjection
import io.boardproxy.control.runtime.domain.RuntimeSnapshot
import io.boardproxy.control.runtime.domain.RuntimeUserSnapshot
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith

class RuntimeProjectionRebuildServiceTest {
    private val now = Instant.parse("2026-08-12T12:00:00Z")

    @Test
    fun `rebuild starts from authoritative snapshot and replays later facts`() {
        val store = Store()
        val snapshot = RuntimeSnapshot("boot-1", 10, 2, now, listOf(RuntimeUserSnapshot("alice", 0, 10, 20)), emptyList())
        val event = RuntimeEvent(
            "event-11", "boot-1", 11, now.plusSeconds(1), 3,
            RuntimeEventPayload.ClientSessionOpened("alice", "main", "bundle-1"),
        )
        val service = RuntimeProjectionRebuildService(
            store, RuntimeReplayStore { RuntimeReplayMaterial(snapshot, listOf(event)) }, DirectTransactions,
            RuntimeProjectionNotifier {},
        )

        val rebuilt = service.rebuild("node-1")

        assertEquals(11, rebuilt.lastSequence)
        assertEquals(1, rebuilt.users.getValue("alice").activeSessions)
        assertEquals(8, rebuilt.version)
    }

    @Test
    fun `rebuild refuses to invent state without snapshot`() {
        val service = RuntimeProjectionRebuildService(
            Store(), RuntimeReplayStore { null }, DirectTransactions, RuntimeProjectionNotifier {},
        )
        assertFailsWith<ResourceConflict> { service.rebuild("node-1") }
    }

    private class Store : RuntimeEventStore {
        private var projection = RuntimeProjection("node-1", version = 7)
        override fun claimBatch(batch: RuntimeEventBatch) = error("not used")
        override fun appendEvent(nodeId: String, event: RuntimeEvent) = error("not used")
        override fun lockProjection(nodeId: String) = projection
        override fun saveProjection(projection: RuntimeProjection) { this.projection = projection }
    }

    private object DirectTransactions : TransactionRunner {
        override fun <T> required(block: () -> T): T = block()
    }
}
