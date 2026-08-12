package io.boardproxy.control.delivery.infrastructure.events

import io.boardproxy.control.shared.events.ControlEvent
import io.boardproxy.control.shared.events.LocalControlEventBus
import kotlinx.coroutines.async
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.yield
import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals

class DesiredRevisionEventBusTest {
    @Test
    fun `only matching desired event wakes node stream`() = runBlocking {
        val local = LocalControlEventBus()
        val signals = DesiredRevisionEventBus(local)
        val received = async(start = CoroutineStart.UNDISPATCHED) {
            withTimeout(1_000) { signals.changes("node-1").first() }
        }
        yield()

        local.publish(ControlEvent("node.status.changed", "node-1", occurredAt = Instant.EPOCH))
        local.publish(ControlEvent("desired-state.changed", "node-2", occurredAt = Instant.EPOCH))
        local.publish(ControlEvent("desired-state.changed", "node-1", occurredAt = Instant.EPOCH))

        assertEquals(Unit, received.await())
    }
}
