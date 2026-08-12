package io.boardproxy.control.runtime.infrastructure.events

import io.boardproxy.control.runtime.application.RuntimeProjectionNotifier
import io.boardproxy.control.runtime.domain.RuntimeProjection
import io.boardproxy.control.shared.events.ControlEvent
import io.boardproxy.control.shared.events.DistributedControlEventPublisher
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component
import java.time.Clock

@Component
class DistributedRuntimeProjectionNotifier(
    private val events: DistributedControlEventPublisher,
    private val clock: Clock,
) : RuntimeProjectionNotifier {
    override fun changed(projection: RuntimeProjection) {
        runCatching {
            events.publish(
                ControlEvent(
                    type = "runtime.projection.changed",
                    aggregateId = projection.nodeId,
                    payload = mapOf(
                        "nodeId" to projection.nodeId,
                        "coreBootId" to (projection.coreBootId ?: ""),
                        "lastSequence" to projection.lastSequence,
                        "runtimeRevision" to projection.runtimeRevision,
                        "gapDetected" to projection.gapDetected,
                        "sessionDetailsComplete" to projection.sessionDetailsComplete,
                        "version" to projection.version,
                    ),
                    occurredAt = clock.instant(),
                ),
            )
        }.onFailure { error ->
            logger.warn("failed to publish runtime projection event for {}", projection.nodeId, error)
        }
    }

    private companion object {
        val logger = LoggerFactory.getLogger(DistributedRuntimeProjectionNotifier::class.java)
    }
}
