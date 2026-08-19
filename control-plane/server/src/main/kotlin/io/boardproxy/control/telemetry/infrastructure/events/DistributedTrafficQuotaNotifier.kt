package io.boardproxy.control.telemetry.infrastructure.events

import io.boardproxy.control.shared.events.ControlEvent
import io.boardproxy.control.shared.events.DistributedControlEventPublisher
import io.boardproxy.control.telemetry.application.TrafficQuotaNotifier
import io.boardproxy.control.telemetry.domain.TrafficQuotaUsage
import org.springframework.stereotype.Component
import java.time.Clock

@Component
class DistributedTrafficQuotaNotifier(
    private val events: DistributedControlEventPublisher,
    private val clock: Clock,
) : TrafficQuotaNotifier {
    override fun exceeded(usage: TrafficQuotaUsage) {
        runCatching {
            events.publish(
                ControlEvent(
                    "traffic.quota.exceeded",
                    "user:${usage.quota.userId}",
                    mapOf(
                        "userId" to usage.quota.userId,
                        "usedBytes" to usage.usedBytes,
                        "limitBytes" to usage.quota.limitBytes,
                        "action" to usage.quota.action.name.lowercase(),
                    ),
                    clock.instant(),
                ),
            )
        }
    }
}
