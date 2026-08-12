package io.boardproxy.control.shared.events.postgres

import io.boardproxy.control.shared.events.OutboxDeliveryRepository
import org.slf4j.LoggerFactory
import org.springframework.scheduling.annotation.Scheduled
import org.springframework.stereotype.Component

@Component
class PostgresOutboxPublisher(private val outbox: OutboxDeliveryRepository) {
    @Scheduled(fixedDelayString = "\${control.events.outbox-delay:100}")
    fun publish() {
        runCatching {
            while (outbox.publishPending(BATCH_SIZE) == BATCH_SIZE) {
                // Drain a bounded page at a time while backlog exists.
            }
        }.onFailure { error -> logger.warn("failed to publish control outbox", error) }
    }

    private companion object {
        const val BATCH_SIZE = 100
        val logger = LoggerFactory.getLogger(PostgresOutboxPublisher::class.java)
    }
}
