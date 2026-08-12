package io.boardproxy.control.delivery.infrastructure.events

import io.boardproxy.control.delivery.application.DesiredRevisionSignals
import io.boardproxy.control.shared.events.LocalControlEventBus
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.callbackFlow
import org.springframework.stereotype.Component

@Component
class DesiredRevisionEventBus(private val events: LocalControlEventBus) : DesiredRevisionSignals {
    override fun changes(nodeId: String): Flow<Unit> = callbackFlow {
        val subscription = events.subscribe { event ->
            if (event.type == DESIRED_STATE_CHANGED && event.aggregateId == nodeId) trySend(Unit)
        }
        awaitClose(subscription::close)
    }

    private companion object {
        const val DESIRED_STATE_CHANGED = "desired-state.changed"
    }
}
