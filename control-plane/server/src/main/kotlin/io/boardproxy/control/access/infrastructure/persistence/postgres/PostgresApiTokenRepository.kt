package io.boardproxy.control.access.infrastructure.persistence.postgres

import io.boardproxy.control.access.application.ApiTokenRepository
import io.boardproxy.control.access.domain.AccessRole
import io.boardproxy.control.access.domain.ApiToken
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet
import java.time.Instant

@Repository
class PostgresApiTokenRepository(private val jdbc: NamedParameterJdbcTemplate) : ApiTokenRepository {
    override fun create(token: ApiToken) {
        jdbc.update(
            """
            INSERT INTO api_tokens (
                id, name, token_hash, role, created_by, created_at, expires_at, revoked_at
            ) VALUES (
                :id, :name, :tokenHash, :role, :createdBy, :createdAt, :expiresAt, :revokedAt
            )
            """.trimIndent(),
            mapOf(
                "id" to token.id, "name" to token.name, "tokenHash" to token.tokenHash,
                "role" to token.role.name.lowercase(), "createdBy" to token.createdBy,
                "createdAt" to token.createdAt.toSqlTimestamp(),
                "expiresAt" to token.expiresAt?.toSqlTimestamp(),
                "revokedAt" to token.revokedAt?.toSqlTimestamp(),
            ),
        )
    }

    override fun findActiveByHash(tokenHash: String, now: Instant): ApiToken? = jdbc.query(
        """
        SELECT id, name, token_hash, role, created_by, created_at, expires_at, revoked_at, last_used_at
        FROM api_tokens
        WHERE token_hash = :tokenHash AND revoked_at IS NULL
          AND (expires_at IS NULL OR expires_at > :now)
        """.trimIndent(),
        mapOf("tokenHash" to tokenHash, "now" to now.toSqlTimestamp()),
    ) { rs, _ -> map(rs) }.firstOrNull()

    override fun list(): List<ApiToken> = jdbc.query(
        """
        SELECT id, name, token_hash, role, created_by, created_at, expires_at, revoked_at, last_used_at
        FROM api_tokens ORDER BY created_at DESC
        """.trimIndent(),
        emptyMap<String, Any>(),
    ) { rs, _ -> map(rs) }

    override fun revoke(id: String, revokedAt: Instant): Boolean = jdbc.update(
        """
        UPDATE api_tokens SET revoked_at = :revokedAt
        WHERE id = :id AND revoked_at IS NULL
        """.trimIndent(),
        mapOf("id" to id, "revokedAt" to revokedAt.toSqlTimestamp()),
    ) == 1

    override fun touch(id: String, usedAt: Instant) {
        jdbc.update(
            """
            UPDATE api_tokens SET last_used_at = :usedAt
            WHERE id = :id AND (last_used_at IS NULL OR last_used_at < :threshold)
            """.trimIndent(),
            mapOf(
                "id" to id, "usedAt" to usedAt.toSqlTimestamp(),
                "threshold" to usedAt.minusSeconds(300).toSqlTimestamp(),
            ),
        )
    }

    private fun map(rs: ResultSet) = ApiToken(
        id = rs.getString("id"), name = rs.getString("name"), tokenHash = rs.getString("token_hash"),
        role = AccessRole.valueOf(rs.getString("role").uppercase()), createdBy = rs.getString("created_by"),
        createdAt = rs.getTimestamp("created_at").toInstant(),
        expiresAt = rs.getTimestamp("expires_at")?.toInstant(),
        revokedAt = rs.getTimestamp("revoked_at")?.toInstant(),
        lastUsedAt = rs.getTimestamp("last_used_at")?.toInstant(),
    )
}
