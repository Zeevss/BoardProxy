package io.boardproxy.control.delivery.infrastructure.events

import io.boardproxy.control.delivery.application.DesiredRevisionSignals
import io.boardproxy.control.delivery.application.RevisionSubscription
import io.boardproxy.control.shared.events.LocalControlEventBus
import org.springframework.stereotype.Component
import java.time.Duration
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.TimeUnit

@Component
class DesiredRevisionEventBus(private val events: LocalControlEventBus) : DesiredRevisionSignals {

    override fun subscribe(nodeId: String): RevisionSubscription {
        // Очередь на один элемент: важен факт изменения, а не их количество.
        // Нода всё равно читает актуальную ревизию, а не отслеживает каждую.
        val signals = ArrayBlockingQueue<Unit>(1)
        val subscription = events.subscribe { event ->
            if (event.type == DESIRED_STATE_CHANGED && event.aggregateId == nodeId) signals.offer(Unit)
        }
        return object : RevisionSubscription {
            override fun await(timeout: Duration): Boolean =
                signals.poll(timeout.toMillis(), TimeUnit.MILLISECONDS) != null

            override fun close() = subscription.close()
        }
    }

    private companion object {
        const val DESIRED_STATE_CHANGED = "desired-state.changed"
    }
}
