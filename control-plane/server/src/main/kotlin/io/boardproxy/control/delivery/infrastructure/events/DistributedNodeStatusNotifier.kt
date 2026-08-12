package io.boardproxy.control.delivery.infrastructure.events

import io.boardproxy.control.delivery.application.NodeStatusNotifier
import io.boardproxy.control.delivery.domain.NodeStatus
import io.boardproxy.control.shared.events.ControlEvent
import io.boardproxy.control.shared.events.DistributedControlEventPublisher
import org.springframework.stereotype.Component
import org.slf4j.LoggerFactory
import java.time.Clock

@Component
class DistributedNodeStatusNotifier(
    private val events: DistributedControlEventPublisher,
    private val clock: Clock,
) : NodeStatusNotifier {
    override fun changed(status: NodeStatus) {
        runCatching {
            events.publish(
                ControlEvent(
                    type = "node.status.changed",
                    aggregateId = status.nodeId,
                    payload = mapOf(
                        "connected" to status.connected,
                        "coreRunning" to status.coreRunning,
                        "coreReady" to status.coreReady,
                        "desiredRevision" to status.desiredRevision,
                        "appliedRevision" to status.appliedRevision,
                        "version" to status.version,
                    ),
                    occurredAt = clock.instant(),
                ),
            )
        }.onFailure { error -> logger.warn("failed to publish node status event for {}", status.nodeId, error) }
    }

    private companion object {
        val logger = LoggerFactory.getLogger(DistributedNodeStatusNotifier::class.java)
    }
}
