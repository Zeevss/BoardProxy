package io.boardproxy.control.shared.events

import java.time.Instant

data class ControlEvent(
    val type: String,
    val aggregateId: String,
    val payload: Map<String, Any> = emptyMap(),
    val occurredAt: Instant,
)

fun interface DistributedControlEventPublisher {
    fun publish(event: ControlEvent)
}
