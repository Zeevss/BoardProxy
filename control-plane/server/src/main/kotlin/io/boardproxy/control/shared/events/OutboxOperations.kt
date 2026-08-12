package io.boardproxy.control.shared.events

import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceNotFound

class OutboxOperations(private val repository: OutboxDeliveryRepository) {
    fun deadLetters(limit: Int): List<OutboxDeadLetter> {
        if (limit !in 1..500) throw InvalidRequest("outbox dead-letter limit must be between 1 and 500")
        return repository.deadLetters(limit)
    }

    fun retry(eventId: String) {
        if (eventId.isBlank()) throw InvalidRequest("outbox event id is required")
        if (!repository.retry(eventId)) throw ResourceNotFound("dead-lettered outbox event not found")
    }
}
