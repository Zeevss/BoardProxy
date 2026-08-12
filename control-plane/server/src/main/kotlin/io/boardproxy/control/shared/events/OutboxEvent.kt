package io.boardproxy.control.shared.events

import java.time.Instant

data class OutboxEvent(
    val id: String,
    val aggregateType: String,
    val aggregateId: String,
    val type: String,
    val payload: Map<String, Any>,
    val occurredAt: Instant,
)

fun interface OutboxRepository {
    fun append(event: OutboxEvent)
}

interface OutboxDeliveryRepository {
    fun publishPending(limit: Int): Int
    fun find(eventId: String): OutboxEvent?
    fun deadLetters(limit: Int): List<OutboxDeadLetter>
    fun retry(eventId: String): Boolean
}

data class OutboxDeadLetter(
    val event: OutboxEvent,
    val attempts: Int,
    val lastError: String?,
    val deadLetteredAt: Instant,
)
