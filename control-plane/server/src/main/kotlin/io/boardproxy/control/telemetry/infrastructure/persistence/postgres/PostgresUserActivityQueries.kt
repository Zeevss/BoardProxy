package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.shared.contracts.UserActivityQueries
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Component

/**
 * Последняя активность каждого пользователя одним запросом.
 *
 * Источник — история дельт, а не текущий runtime-снимок: снимок описывает
 * происходящее сейчас, поэтому вчерашний пользователь в нём отсутствует и
 * выглядел бы никогда не подключавшимся.
 */
@Component
class PostgresUserActivityQueries(
    private val jdbc: NamedParameterJdbcTemplate,
) : UserActivityQueries {

    override fun lastSeen(): Map<String, java.time.Instant> = jdbc.query(
        "SELECT user_id, MAX(observed_at) AS last_seen FROM user_traffic_deltas GROUP BY user_id",
        emptyMap<String, Any>(),
    ) { rs, _ -> rs.getString("user_id") to rs.getTimestamp("last_seen").toInstant() }.toMap()
}
