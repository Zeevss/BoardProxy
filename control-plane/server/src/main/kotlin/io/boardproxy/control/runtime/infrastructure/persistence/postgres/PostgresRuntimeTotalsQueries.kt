package io.boardproxy.control.runtime.infrastructure.persistence.postgres

import io.boardproxy.control.shared.contracts.RuntimeTotals
import io.boardproxy.control.shared.contracts.RuntimeTotalsQueries
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository

/**
 * Итоги снимков по всему флоту одним запросом.
 *
 * Свёртка делается в базе, а не в приложении: иначе список нод тянул бы в
 * память полный снимок каждой — со всеми пользователями и бордами — ради двух
 * чисел.
 *
 * `LEFT JOIN LATERAL` оставляет ноду в выдаче и тогда, когда снимок пуст: ядро
 * без активных сессий — это ноль, а не отсутствующая строка.
 */
@Repository
class PostgresRuntimeTotalsQueries(
    private val jdbc: NamedParameterJdbcTemplate,
) : RuntimeTotalsQueries {

    override fun all(): Map<String, RuntimeTotals> = jdbc.query(
        """
        SELECT r.node_id,
               COALESCE(SUM((u ->> 'activeSessions')::int), 0) AS sessions,
               COALESCE(SUM((u ->> 'activeLanes')::int), 0)    AS lanes,
               r.observed_at
        FROM node_runtime r
        LEFT JOIN LATERAL jsonb_array_elements(r.snapshot -> 'users') AS u ON true
        GROUP BY r.node_id, r.observed_at
        """.trimIndent(),
        emptyMap<String, Any>(),
    ) { rs, _ ->
        rs.getString("node_id") to RuntimeTotals(
            activeSessions = rs.getInt("sessions"),
            activeLanes = rs.getInt("lanes"),
            observedAt = rs.getTimestamp("observed_at").toInstant(),
        )
    }.toMap()
}
