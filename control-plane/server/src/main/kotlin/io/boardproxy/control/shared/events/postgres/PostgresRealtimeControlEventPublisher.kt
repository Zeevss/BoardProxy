package io.boardproxy.control.shared.events.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.shared.events.ControlEvent
import io.boardproxy.control.shared.events.DistributedControlEventPublisher
import org.springframework.jdbc.core.JdbcTemplate
import org.springframework.stereotype.Component

@Component
class PostgresRealtimeControlEventPublisher(
    private val jdbc: JdbcTemplate,
    private val json: ObjectMapper,
) : DistributedControlEventPublisher {
    override fun publish(event: ControlEvent) {
        jdbc.queryForObject(
            "SELECT pg_notify(?, ?)",
            String::class.java,
            PostgresOutboxRepository.CHANNEL,
            json.writeValueAsString(PostgresOutboxRepository.NotificationEnvelope.realtime(event)),
        )
    }
}
