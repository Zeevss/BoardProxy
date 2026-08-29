package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.shared.contracts.UserUsage
import io.boardproxy.control.shared.contracts.UserUsageQueries
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.telemetry.application.TrafficKind
import io.boardproxy.control.telemetry.application.quotaWindow
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Component
import java.time.Clock
import java.time.Instant

/**
 * Расход пользователя одним индексированным запросом.
 *
 * Прежде страница подписки вытягивала тоталы всех пользователей каждой ноды от
 * начала времён, чтобы прочитать одного.
 */
@Component
class PostgresUserUsageQueries(
    private val jdbc: NamedParameterJdbcTemplate,
    private val clock: Clock,
) : UserUsageQueries {

    override fun usage(userId: String): UserUsage {
        val quota = jdbc.query(
            """
            SELECT period, limit_bytes, counter_start
            FROM user_traffic_quotas WHERE user_id = :userId AND enabled
            """.trimIndent(),
            mapOf("userId" to userId),
        ) { rs, _ ->
            Triple(
                QuotaPeriod.valueOf(rs.getString("period").uppercase()),
                rs.getLong("limit_bytes"),
                rs.getTimestamp("counter_start")?.toInstant(),
            )
        }.firstOrNull()
        val now = clock.instant()
        val window = quota?.let { quotaWindow(it.first, now) } ?: (Instant.EPOCH to now)
        val from = maxOf(window.first, quota?.third ?: window.first)
        // PostgreSQL timestamptz хранит микросекунды: 1ns округлился бы назад.
        val to = minOf(window.second, now.plusNanos(1_000))

        val perNode = jdbc.query(
            PostgresTrafficSources.cte(TrafficKind.USER, null) + """
                SELECT node_id, SUM(used) AS used
                FROM (
                    SELECT node_id, SUM(rx_bytes + tx_bytes) AS used
                    FROM combined_traffic WHERE subject = :userId
                    GROUP BY node_id
                    UNION ALL
                    SELECT node_id, rx_bytes + tx_bytes AS used
                    FROM user_traffic_lifetime_totals
                    WHERE user_id = :userId AND CAST(:includeLifetime AS boolean)
                ) usage_by_source
                GROUP BY node_id ORDER BY node_id
            """.trimIndent(),
            mapOf(
                "nodeId" to null,
                "userId" to userId,
                "from" to from.toSqlTimestamp(),
                "to" to to.toSqlTimestamp(),
                "useRollups" to true,
                "includeLifetime" to (from == Instant.EPOCH),
            ),
        ) { rs, _ -> rs.getString("node_id") to rs.getLong("used") }.toMap()

        return UserUsage(limitBytes = quota?.second ?: 0, usedBytes = perNode.values.sum(), perNode = perNode)
    }
}
