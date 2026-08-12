package io.boardproxy.control.delivery.infrastructure.persistence.postgres

import io.boardproxy.control.delivery.application.NodeSessionLease
import io.boardproxy.control.delivery.application.NodeSessionLeaseRepository
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import org.springframework.transaction.annotation.Transactional
import java.time.Duration
import java.time.Instant

@Repository
class PostgresNodeSessionLeaseRepository(
    private val jdbc: NamedParameterJdbcTemplate,
) : NodeSessionLeaseRepository {
    override fun acquire(
        nodeId: String,
        ownerId: String,
        sessionId: String,
        now: Instant,
        ttl: Duration,
    ): NodeSessionLease? = jdbc.query(
        """
        INSERT INTO node_session_leases (
            node_id, owner_id, session_id, fencing_token, acquired_at, expires_at
        ) VALUES (:nodeId, :ownerId, :sessionId, 1, :now, :expiresAt)
        ON CONFLICT (node_id) DO UPDATE SET
            owner_id = EXCLUDED.owner_id,
            session_id = EXCLUDED.session_id,
            fencing_token = node_session_leases.fencing_token + 1,
            acquired_at = EXCLUDED.acquired_at,
            expires_at = EXCLUDED.expires_at
        WHERE node_session_leases.expires_at <= :now
        RETURNING node_id, owner_id, session_id, fencing_token, expires_at
        """.trimIndent(),
        parameters(nodeId, ownerId, sessionId, now, ttl),
    ) { rs, _ ->
        NodeSessionLease(
            rs.getString("node_id"), rs.getString("owner_id"), rs.getString("session_id"),
            rs.getLong("fencing_token"), rs.getTimestamp("expires_at").toInstant(),
        )
    }.firstOrNull()

    override fun renew(
        lease: NodeSessionLease,
        now: Instant,
        ttl: Duration,
    ): NodeSessionLease? = jdbc.query(
        """
        UPDATE node_session_leases SET expires_at = :expiresAt
        WHERE node_id = :nodeId AND owner_id = :ownerId AND session_id = :sessionId
          AND fencing_token = :fencingToken AND expires_at > :now
        RETURNING node_id, owner_id, session_id, fencing_token, expires_at
        """.trimIndent(),
        parameters(lease.nodeId, lease.ownerId, lease.sessionId, now, ttl) +
            ("fencingToken" to lease.fencingToken),
    ) { rs, _ ->
        lease.copy(expiresAt = rs.getTimestamp("expires_at").toInstant())
    }.firstOrNull()

    override fun release(lease: NodeSessionLease) {
        jdbc.update(
            """
            DELETE FROM node_session_leases
            WHERE node_id = :nodeId AND owner_id = :ownerId
              AND session_id = :sessionId AND fencing_token = :fencingToken
            """.trimIndent(),
            mapOf(
                "nodeId" to lease.nodeId, "ownerId" to lease.ownerId,
                "sessionId" to lease.sessionId, "fencingToken" to lease.fencingToken,
            ),
        )
    }

    @Transactional
    override fun expireStatuses(now: Instant): Int = jdbc.update(
        """
        UPDATE node_status status SET connected = false, core_ready = false,
            projection_version = projection_version + 1
        WHERE connected = true AND last_seen < :cutoff
          AND NOT EXISTS (
              SELECT 1 FROM node_session_leases lease
              WHERE lease.node_id = status.node_id AND lease.expires_at > :now
          )
        """.trimIndent(),
        mapOf("now" to now.toSqlTimestamp(), "cutoff" to now.minusSeconds(45).toSqlTimestamp()),
    )

    private fun parameters(nodeId: String, ownerId: String, sessionId: String, now: Instant, ttl: Duration) = mapOf(
        "nodeId" to nodeId, "ownerId" to ownerId, "sessionId" to sessionId,
        "now" to now.toSqlTimestamp(), "expiresAt" to now.plus(ttl).toSqlTimestamp(),
    )
}
