package io.boardproxy.control.shared.events.postgres

import io.boardproxy.control.shared.events.OutboxDeliveryRepository
import io.boardproxy.control.shared.events.OutboxEvent
import kotlin.test.Test
import kotlin.test.assertEquals

class PostgresOutboxPublisherTest {
    @Test
    fun `publisher drains backlog in bounded pages`() {
        val repository = FakeDeliveryRepository(remaining = 205)

        PostgresOutboxPublisher(repository).publish()

        assertEquals(0, repository.remaining)
        assertEquals(listOf(100, 100, 100), repository.requestedLimits)
    }

    private class FakeDeliveryRepository(var remaining: Int) : OutboxDeliveryRepository {
        val requestedLimits = mutableListOf<Int>()

        override fun publishPending(limit: Int): Int {
            requestedLimits += limit
            return minOf(limit, remaining).also { remaining -= it }
        }

        override fun find(eventId: String): OutboxEvent? = null
        override fun deadLetters(limit: Int) = emptyList<io.boardproxy.control.shared.events.OutboxDeadLetter>()
        override fun retry(eventId: String) = false
    }
}
