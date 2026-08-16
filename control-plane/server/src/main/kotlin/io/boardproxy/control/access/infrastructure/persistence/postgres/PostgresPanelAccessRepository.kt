package io.boardproxy.control.access.infrastructure.persistence.postgres

import io.boardproxy.control.access.application.PanelAccessRepository
import io.boardproxy.control.access.domain.PanelAdministrator
import io.boardproxy.control.access.domain.PanelSession
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet
import java.time.Instant

@Repository
class PostgresPanelAccessRepository(private val jdbc: NamedParameterJdbcTemplate) : PanelAccessRepository {
    override fun administrator(): PanelAdministrator? = jdbc.query(
        """
        SELECT username, password_hash, created_at, updated_at
        FROM panel_administrators WHERE singleton = true
        """.trimIndent(),
        emptyMap<String, Any>(),
    ) { rs, _ -> administrator(rs) }.firstOrNull()

    override fun createAdministrator(administrator: PanelAdministrator): Boolean = jdbc.update(
        """
        INSERT INTO panel_administrators (singleton, username, password_hash, created_at, updated_at)
        VALUES (true, :username, :passwordHash, :createdAt, :updatedAt)
        ON CONFLICT (singleton) DO NOTHING
        """.trimIndent(),
        mapOf(
            "username" to administrator.username,
            "passwordHash" to administrator.passwordHash,
            "createdAt" to administrator.createdAt.toSqlTimestamp(),
            "updatedAt" to administrator.updatedAt.toSqlTimestamp(),
        ),
    ) == 1

    override fun createSession(session: PanelSession) {
        jdbc.update(
            """
            INSERT INTO panel_sessions (
                id, administrator, token_hash, created_at, expires_at, last_used_at, revoked_at
            ) VALUES (
                :id, true, :tokenHash, :createdAt, :expiresAt, :lastUsedAt, :revokedAt
            )
            """.trimIndent(),
            mapOf(
                "id" to session.id,
                "tokenHash" to session.tokenHash,
                "createdAt" to session.createdAt.toSqlTimestamp(),
                "expiresAt" to session.expiresAt.toSqlTimestamp(),
                "lastUsedAt" to session.lastUsedAt?.toSqlTimestamp(),
                "revokedAt" to session.revokedAt?.toSqlTimestamp(),
            ),
        )
    }

    override fun findActiveSessionByHash(tokenHash: String, now: Instant): PanelSession? = jdbc.query(
        """
        SELECT s.id, s.token_hash, a.username, s.created_at, s.expires_at, s.last_used_at, s.revoked_at
        FROM panel_sessions s
        JOIN panel_administrators a ON a.singleton = s.administrator
        WHERE s.token_hash = :tokenHash AND s.revoked_at IS NULL AND s.expires_at > :now
        """.trimIndent(),
        mapOf("tokenHash" to tokenHash, "now" to now.toSqlTimestamp()),
    ) { rs, _ -> session(rs) }.firstOrNull()

    override fun touchSession(id: String, usedAt: Instant) {
        jdbc.update(
            """
            UPDATE panel_sessions SET last_used_at = :usedAt
            WHERE id = :id AND (last_used_at IS NULL OR last_used_at < :threshold)
            """.trimIndent(),
            mapOf(
                "id" to id,
                "usedAt" to usedAt.toSqlTimestamp(),
                "threshold" to usedAt.minusSeconds(300).toSqlTimestamp(),
            ),
        )
    }

    override fun revokeSessionByHash(tokenHash: String, revokedAt: Instant): Boolean = jdbc.update(
        """
        UPDATE panel_sessions SET revoked_at = :revokedAt
        WHERE token_hash = :tokenHash AND revoked_at IS NULL
        """.trimIndent(),
        mapOf("tokenHash" to tokenHash, "revokedAt" to revokedAt.toSqlTimestamp()),
    ) == 1

    private fun administrator(rs: ResultSet) = PanelAdministrator(
        username = rs.getString("username"),
        passwordHash = rs.getString("password_hash"),
        createdAt = rs.getTimestamp("created_at").toInstant(),
        updatedAt = rs.getTimestamp("updated_at").toInstant(),
    )

    private fun session(rs: ResultSet) = PanelSession(
        id = rs.getString("id"),
        tokenHash = rs.getString("token_hash"),
        username = rs.getString("username"),
        createdAt = rs.getTimestamp("created_at").toInstant(),
        expiresAt = rs.getTimestamp("expires_at").toInstant(),
        lastUsedAt = rs.getTimestamp("last_used_at")?.toInstant(),
        revokedAt = rs.getTimestamp("revoked_at")?.toInstant(),
    )
}
