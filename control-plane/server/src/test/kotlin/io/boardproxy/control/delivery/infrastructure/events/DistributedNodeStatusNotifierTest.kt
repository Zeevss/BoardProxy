package io.boardproxy.control.delivery.infrastructure.events

import io.boardproxy.control.delivery.domain.NodeStatus
import io.boardproxy.control.shared.events.DistributedControlEventPublisher
import java.time.Clock
import kotlin.test.Test

class DistributedNodeStatusNotifierTest {
    @Test
    fun `notification failure never escapes into node session`() {
        val notifier = DistributedNodeStatusNotifier(
            DistributedControlEventPublisher { error("PostgreSQL unavailable") },
            Clock.systemUTC(),
        )

        notifier.changed(NodeStatus("node-1", connected = true))
    }
}
