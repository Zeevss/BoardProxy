package io.boardproxy.control.shared.events

import org.springframework.stereotype.Component
import org.slf4j.LoggerFactory
import java.util.concurrent.CopyOnWriteArraySet

@Component
class LocalControlEventBus {
    private val listeners = CopyOnWriteArraySet<(ControlEvent) -> Unit>()

    fun publish(event: ControlEvent) {
        listeners.forEach { listener ->
            runCatching { listener(event) }
                .onFailure { error -> logger.warn("local control-event subscriber failed", error) }
        }
    }

    fun subscribe(listener: (ControlEvent) -> Unit): AutoCloseable {
        listeners += listener
        return AutoCloseable { listeners -= listener }
    }

    private companion object {
        val logger = LoggerFactory.getLogger(LocalControlEventBus::class.java)
    }
}
