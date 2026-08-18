package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.telemetry.application.TrafficQuotaRepository
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import io.boardproxy.control.telemetry.domain.TrafficQuota
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet
import java.time.Instant

@Repository
class PostgresTrafficQuotaRepository(private val jdbc: NamedParameterJdbcTemplate) : TrafficQuotaRepository {
    override fun find(nodeId: String, userTag: String): TrafficQuota? = jdbc.query(
        "$SELECT WHERE node_id = :nodeId AND user_tag = :userTag",
        mapOf("nodeId" to nodeId, "userTag" to userTag),
    ) { rs, _ -> map(rs) }.firstOrNull()

    override fun list(nodeId: String): List<TrafficQuota> = jdbc.query(
        "$SELECT WHERE node_id = :nodeId ORDER BY user_tag",
        mapOf("nodeId" to nodeId),
    ) { rs, _ -> map(rs) }

    override fun enabled(): List<TrafficQuota> = jdbc.query(
        "$SELECT WHERE enabled = true ORDER BY node_id, user_tag",
        emptyMap<String, Any>(),
    ) { rs, _ -> map(rs) }

    override fun save(quota: TrafficQuota, expectedVersion: Long?): Boolean {
        if (expectedVersion == null) return jdbc.update(
            """
            INSERT INTO user_traffic_quotas (
                node_id, user_tag, period, limit_bytes, action, enabled, resource_version, updated_at, counter_start
            ) VALUES (:nodeId, :userTag, :period, :limitBytes, :action, :enabled, :version, :updatedAt, :counterStart)
            ON CONFLICT (node_id, user_tag) DO NOTHING
            """.trimIndent(),
            parameters(quota),
        ) == 1
        return jdbc.update(
            """
            UPDATE user_traffic_quotas SET
                period = :period, limit_bytes = :limitBytes, action = :action,
                enabled = :enabled, resource_version = :version, updated_at = :updatedAt,
                counter_start = :counterStart
            WHERE node_id = :nodeId AND user_tag = :userTag AND resource_version = :expectedVersion
            """.trimIndent(),
            parameters(quota) + ("expectedVersion" to expectedVersion),
        ) == 1
    }

    override fun delete(nodeId: String, userTag: String, expectedVersion: Long): Boolean = jdbc.update(
        """
        DELETE FROM user_traffic_quotas
        WHERE node_id = :nodeId AND user_tag = :userTag AND resource_version = :expectedVersion
        """.trimIndent(),
        mapOf("nodeId" to nodeId, "userTag" to userTag, "expectedVersion" to expectedVersion),
    ) == 1

    override fun usedBytes(nodeId: String, userTag: String, from: Instant, to: Instant): Long = requireNotNull(
        jdbc.queryForObject(
            """
            SELECT COALESCE(SUM(delta.rx_bytes + delta.tx_bytes), 0)
            FROM user_traffic_deltas delta
            JOIN traffic_batches batch USING (node_id, batch_id)
            WHERE delta.node_id = :nodeId AND delta.user_tag = :userTag
              AND batch.interval_end > :from AND batch.interval_end <= :to
            """.trimIndent(),
            mapOf(
                "nodeId" to nodeId, "userTag" to userTag,
                "from" to from.toSqlTimestamp(), "to" to to.toSqlTimestamp(),
            ),
            Long::class.java,
        ),
    )

    override fun recordExceeded(nodeId: String, userTag: String, periodStart: Instant, at: Instant): Boolean =
        jdbc.update(
            """
            INSERT INTO user_traffic_quota_state (node_id, user_tag, period_start, exceeded_at)
            VALUES (:nodeId, :userTag, :periodStart, :at)
            ON CONFLICT (node_id, user_tag, period_start) DO NOTHING
            """.trimIndent(),
            stateParameters(nodeId, userTag, periodStart) + ("at" to at.toSqlTimestamp()),
        ) == 1

    override fun recordEnforced(nodeId: String, userTag: String, periodStart: Instant, at: Instant) {
        jdbc.update(
            """
            UPDATE user_traffic_quota_state SET enforced_at = :at
            WHERE node_id = :nodeId AND user_tag = :userTag AND period_start = :periodStart
            """.trimIndent(),
            stateParameters(nodeId, userTag, periodStart) + ("at" to at.toSqlTimestamp()),
        )
    }

    override fun startNewCounter(nodeId: String, userTag: String, at: Instant) {
        jdbc.update(
            "UPDATE user_traffic_quotas SET counter_start = :at WHERE node_id = :nodeId AND user_tag = :userTag",
            mapOf("nodeId" to nodeId, "userTag" to userTag, "at" to at.toSqlTimestamp()),
        )
    }

    override fun state(nodeId: String, userTag: String, periodStart: Instant): Pair<Instant?, Instant?> = jdbc.query(
        """
        SELECT exceeded_at, enforced_at FROM user_traffic_quota_state
        WHERE node_id = :nodeId AND user_tag = :userTag AND period_start = :periodStart
        """.trimIndent(),
        stateParameters(nodeId, userTag, periodStart),
    ) { rs, _ -> rs.getTimestamp("exceeded_at")?.toInstant() to rs.getTimestamp("enforced_at")?.toInstant() }
        .firstOrNull() ?: (null to null)

    private fun parameters(quota: TrafficQuota) = mapOf(
        "nodeId" to quota.nodeId, "userTag" to quota.userTag,
        "period" to quota.period.name.lowercase(), "limitBytes" to quota.limitBytes,
        "action" to quota.action.name.lowercase(), "enabled" to quota.enabled,
        "version" to quota.version, "updatedAt" to quota.updatedAt.toSqlTimestamp(),
        "counterStart" to quota.counterStart?.toSqlTimestamp(),
    )

    private fun stateParameters(nodeId: String, userTag: String, periodStart: Instant) = mapOf(
        "nodeId" to nodeId, "userTag" to userTag, "periodStart" to periodStart.toSqlTimestamp(),
    )

    private fun map(rs: ResultSet) = TrafficQuota(
        rs.getString("node_id"), rs.getString("user_tag"),
        QuotaPeriod.valueOf(rs.getString("period").uppercase()), rs.getLong("limit_bytes"),
        QuotaAction.valueOf(rs.getString("action").uppercase()), rs.getBoolean("enabled"),
        rs.getLong("resource_version"), rs.getTimestamp("updated_at").toInstant(),
        rs.getTimestamp("counter_start")?.toInstant(),
    )

    private companion object {
        const val SELECT = """
            SELECT node_id, user_tag, period, limit_bytes, action, enabled, resource_version, updated_at,
                   counter_start
            FROM user_traffic_quotas
        """
    }
}
