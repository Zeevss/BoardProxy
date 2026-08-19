package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.telemetry.application.TrafficQuotaRepository
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import io.boardproxy.control.telemetry.domain.TrafficQuota
import io.boardproxy.control.telemetry.domain.TrafficQuotaState
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet
import java.time.Instant

private const val COLUMNS =
    "user_id, period, limit_bytes, action, enabled, counter_start, resource_version, updated_at"

@Repository
class PostgresTrafficQuotaRepository(
    private val jdbc: NamedParameterJdbcTemplate,
) : TrafficQuotaRepository {

    override fun find(userId: String): TrafficQuota? = jdbc.query(
        "SELECT $COLUMNS FROM user_traffic_quotas WHERE user_id = :userId",
        mapOf("userId" to userId),
    ) { rs, _ -> row(rs) }.firstOrNull()

    override fun list(): List<TrafficQuota> = jdbc.query(
        "SELECT $COLUMNS FROM user_traffic_quotas ORDER BY user_id",
        emptyMap<String, Any>(),
    ) { rs, _ -> row(rs) }

    override fun enabled(): List<TrafficQuota> = jdbc.query(
        "SELECT $COLUMNS FROM user_traffic_quotas WHERE enabled ORDER BY user_id",
        emptyMap<String, Any>(),
    ) { rs, _ -> row(rs) }

    override fun save(quota: TrafficQuota, expectedVersion: Long?): Boolean {
        if (expectedVersion == null) {
            return jdbc.update(
                """
                INSERT INTO user_traffic_quotas (
                    user_id, period, limit_bytes, action, enabled, counter_start,
                    resource_version, updated_at
                ) VALUES (
                    :userId, :period, :limitBytes, :action, :enabled, :counterStart,
                    :version, :updatedAt
                )
                ON CONFLICT (user_id) DO NOTHING
                """.trimIndent(),
                parameters(quota),
            ) == 1
        }
        return jdbc.update(
            """
            UPDATE user_traffic_quotas SET
                period = :period, limit_bytes = :limitBytes, action = :action,
                enabled = :enabled, counter_start = :counterStart,
                resource_version = :version, updated_at = :updatedAt
            WHERE user_id = :userId AND resource_version = :expectedVersion
            """.trimIndent(),
            parameters(quota) + ("expectedVersion" to expectedVersion),
        ) == 1
    }

    override fun delete(userId: String, expectedVersion: Long): Boolean = jdbc.update(
        "DELETE FROM user_traffic_quotas WHERE user_id = :userId AND resource_version = :expectedVersion",
        mapOf("userId" to userId, "expectedVersion" to expectedVersion),
    ) == 1

    /**
     * Расход суммируется по всем нодам: пользователь один, значит и счётчик один.
     * Прежде сумма считалась в пределах ноды, а панель складывала результаты.
     */
    override fun usedBytes(userId: String, from: Instant, to: Instant): Long = jdbc.queryForObject(
        """
        SELECT COALESCE(SUM(rx_bytes + tx_bytes), 0)
        FROM user_traffic_deltas
        WHERE user_id = :userId AND observed_at >= :from AND observed_at < :to
        """.trimIndent(),
        mapOf("userId" to userId, "from" to from.toSqlTimestamp(), "to" to to.toSqlTimestamp()),
        Long::class.java,
    ) ?: 0

    override fun state(userId: String): TrafficQuotaState? = jdbc.query(
        "SELECT user_id, period_start, used_bytes, exceeded, changed_at FROM user_traffic_quota_state WHERE user_id = :userId",
        mapOf("userId" to userId),
    ) { rs, _ ->
        TrafficQuotaState(
            userId = rs.getString("user_id"),
            periodStart = rs.getTimestamp("period_start").toInstant(),
            usedBytes = rs.getLong("used_bytes"),
            exceeded = rs.getBoolean("exceeded"),
            changedAt = rs.getTimestamp("changed_at").toInstant(),
        )
    }.firstOrNull()

    /**
     * Возвращает true только когда изменился флаг [TrafficQuotaState.exceeded]
     * или начался новый период: расход обновляется каждую минуту, и событие на
     * каждое обновление означало бы пересборку конфигурации без причины.
     */
    override fun saveState(state: TrafficQuotaState): Boolean {
        val previous = state(state.userId)
        jdbc.update(
            """
            INSERT INTO user_traffic_quota_state (user_id, period_start, used_bytes, exceeded, changed_at)
            VALUES (:userId, :periodStart, :usedBytes, :exceeded, :changedAt)
            ON CONFLICT (user_id) DO UPDATE SET
                period_start = EXCLUDED.period_start,
                used_bytes = EXCLUDED.used_bytes,
                exceeded = EXCLUDED.exceeded,
                changed_at = EXCLUDED.changed_at
            """.trimIndent(),
            mapOf(
                "userId" to state.userId,
                "periodStart" to state.periodStart.toSqlTimestamp(),
                "usedBytes" to state.usedBytes,
                "exceeded" to state.exceeded,
                "changedAt" to state.changedAt.toSqlTimestamp(),
            ),
        )
        return previous == null || previous.exceeded != state.exceeded
    }

    override fun exceededUsers(): Set<String> = jdbc.queryForList(
        "SELECT user_id FROM user_traffic_quota_state WHERE exceeded",
        emptyMap<String, Any>(),
        String::class.java,
    ).toSet()

    override fun startNewCounter(userId: String, at: Instant) {
        jdbc.update(
            "UPDATE user_traffic_quotas SET counter_start = :at WHERE user_id = :userId",
            mapOf("userId" to userId, "at" to at.toSqlTimestamp()),
        )
    }

    private fun parameters(quota: TrafficQuota): Map<String, Any?> = mapOf(
        "userId" to quota.userId,
        "period" to quota.period.name.lowercase(),
        "limitBytes" to quota.limitBytes,
        "action" to quota.action.name.lowercase(),
        "enabled" to quota.enabled,
        "counterStart" to quota.counterStart?.toSqlTimestamp(),
        "version" to quota.version,
        "updatedAt" to quota.updatedAt.toSqlTimestamp(),
    )

    private fun row(rs: ResultSet) = TrafficQuota(
        userId = rs.getString("user_id"),
        period = QuotaPeriod.valueOf(rs.getString("period").uppercase()),
        limitBytes = rs.getLong("limit_bytes"),
        action = QuotaAction.valueOf(rs.getString("action").uppercase()),
        enabled = rs.getBoolean("enabled"),
        version = rs.getLong("resource_version"),
        updatedAt = rs.getTimestamp("updated_at").toInstant(),
        counterStart = rs.getTimestamp("counter_start")?.toInstant(),
    )
}
