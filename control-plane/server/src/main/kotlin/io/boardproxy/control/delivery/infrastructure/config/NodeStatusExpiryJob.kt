package io.boardproxy.control.delivery.infrastructure.config

import io.boardproxy.control.delivery.application.NodeSessionLeaseRepository
import org.springframework.scheduling.annotation.Scheduled
import org.springframework.stereotype.Component
import java.time.Clock

@Component
class NodeStatusExpiryJob(
    private val leases: NodeSessionLeaseRepository,
    private val clock: Clock,
) {
    @Scheduled(fixedDelayString = "\${control.delivery.status-expiry-delay:PT15S}")
    fun expire() {
        leases.expireStatuses(clock.instant())
    }
}
