package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.shared.contracts.PendingQuotaConfigChange
import io.boardproxy.control.shared.contracts.QuotaConfigChangeRepository
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.time.Instant

@Repository
class PostgresQuotaConfigChangeRepository(
    private val jdbc: NamedParameterJdbcTemplate,
) : QuotaConfigChangeRepository {
    override fun mark(userId: String, at: Instant) {
        jdbc.update(
            """
            INSERT INTO quota_config_changes (user_id, generation, changed_at)
            VALUES (:userId, 1, :at)
            ON CONFLICT (user_id) DO UPDATE SET
                generation = quota_config_changes.generation + 1,
                changed_at = EXCLUDED.changed_at
            """.trimIndent(),
            mapOf("userId" to userId, "at" to at.toSqlTimestamp()),
        )
    }

    override fun find(userId: String): PendingQuotaConfigChange? = jdbc.query(
        "SELECT user_id, generation FROM quota_config_changes WHERE user_id = :userId",
        mapOf("userId" to userId),
    ) { rs, _ -> PendingQuotaConfigChange(rs.getString("user_id"), rs.getLong("generation")) }
        .firstOrNull()

    override fun pending(limit: Int): List<PendingQuotaConfigChange> = jdbc.query(
        """
        SELECT user_id, generation FROM quota_config_changes
        ORDER BY changed_at, user_id LIMIT :limit
        """.trimIndent(),
        mapOf("limit" to limit),
    ) { rs, _ -> PendingQuotaConfigChange(rs.getString("user_id"), rs.getLong("generation")) }

    override fun complete(userId: String, generation: Long): Boolean = jdbc.update(
        "DELETE FROM quota_config_changes WHERE user_id = :userId AND generation = :generation",
        mapOf("userId" to userId, "generation" to generation),
    ) == 1
}
