package io.boardproxy.control.shared.events

import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals

class LocalControlEventBusTest {
    @Test
    fun `broken subscriber cannot prevent delivery to other consumers`() {
        val bus = LocalControlEventBus()
        var delivered = 0
        bus.subscribe { error("broken frontend") }
        bus.subscribe { delivered++ }

        bus.publish(ControlEvent("test", "node-1", occurredAt = Instant.EPOCH))

        assertEquals(1, delivered)
    }
}
