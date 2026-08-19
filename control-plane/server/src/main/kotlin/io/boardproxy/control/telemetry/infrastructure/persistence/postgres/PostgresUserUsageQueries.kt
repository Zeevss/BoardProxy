package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.shared.contracts.UserUsage
import io.boardproxy.control.shared.contracts.UserUsageQueries
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Component

/**
 * Расход пользователя одним индексированным запросом.
 *
 * Прежде страница подписки вытягивала тоталы всех пользователей каждой ноды от
 * начала времён, чтобы прочитать одного.
 */
@Component
class PostgresUserUsageQueries(
    private val jdbc: NamedParameterJdbcTemplate,
) : UserUsageQueries {

    override fun usage(userId: String): UserUsage {
        val perNode = jdbc.query(
            """
            SELECT agent_id, COALESCE(SUM(rx_bytes + tx_bytes), 0) AS used
            FROM user_traffic_deltas WHERE user_id = :userId GROUP BY agent_id
            """.trimIndent(),
            mapOf("userId" to userId),
        ) { rs, _ -> rs.getString("agent_id") to rs.getLong("used") }.toMap()

        val limit = jdbc.queryForObject(
            "SELECT COALESCE(MAX(limit_bytes), 0) FROM user_traffic_quotas WHERE user_id = :userId AND enabled",
            mapOf("userId" to userId),
            Long::class.java,
        ) ?: 0

        return UserUsage(limitBytes = limit, usedBytes = perNode.values.sum(), perNode = perNode)
    }
}
