package io.boardproxy.control.access.infrastructure.persistence.postgres

import io.boardproxy.control.access.application.ApiTokenRepository
import io.boardproxy.control.access.application.PanelAccessRepository
import io.boardproxy.control.access.domain.AccessRole
import io.boardproxy.control.access.domain.ApiToken
import io.boardproxy.control.access.domain.PanelAdministrator
import io.boardproxy.control.access.domain.PanelSession
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet
import java.time.Instant

private const val API_TOKEN = "api_token"
private const val PANEL_SESSION = "panel_session"

/**
 * Токен API и сессия панели — одно и то же: хешированный предъявитель с ролью и
 * сроком. Раньше это были две таблицы и два репозитория с почти дословно
 * совпадающим SQL (найти активного по хешу, отметить использование, отозвать).
 *
 * Учётная запись администратора живёт отдельно: у неё нет ни срока, ни отзыва,
 * и предъявляется она паролем, а не токеном.
 */
@Repository
class PostgresCredentialRepository(
    private val jdbc: NamedParameterJdbcTemplate,
) : ApiTokenRepository, PanelAccessRepository {

    // --- токены API ---

    override fun create(token: ApiToken) {
        insert(
            id = token.id, kind = API_TOKEN, subject = token.name, secretHash = token.tokenHash,
            role = token.role.name.lowercase(), createdBy = token.createdBy,
            createdAt = token.createdAt, expiresAt = token.expiresAt,
        )
    }

    override fun findActiveByHash(tokenHash: String, now: Instant): ApiToken? =
        findActive(API_TOKEN, tokenHash, now)?.toApiToken()

    override fun list(): List<ApiToken> = jdbc.query(
        "$SELECT WHERE kind = :kind ORDER BY created_at DESC",
        mapOf("kind" to API_TOKEN),
    ) { rs, _ -> row(rs).toApiToken() }

    override fun revoke(id: String, revokedAt: Instant): Boolean = jdbc.update(
        "UPDATE credentials SET revoked_at = :at WHERE id = :id AND kind = :kind AND revoked_at IS NULL",
        mapOf("id" to id, "kind" to API_TOKEN, "at" to revokedAt.toSqlTimestamp()),
    ) == 1

    override fun touch(id: String, usedAt: Instant) = touchById(id, usedAt)

    // --- сессии панели ---

    override fun createSession(session: PanelSession) {
        insert(
            id = session.id, kind = PANEL_SESSION, subject = session.username, secretHash = session.tokenHash,
            role = AccessRole.ADMIN.name.lowercase(), createdBy = session.username,
            createdAt = session.createdAt, expiresAt = session.expiresAt,
        )
    }

    override fun findActiveSessionByHash(tokenHash: String, now: Instant): PanelSession? =
        findActive(PANEL_SESSION, tokenHash, now)?.toPanelSession()

    override fun touchSession(id: String, usedAt: Instant) = touchById(id, usedAt)

    override fun revokeSessionByHash(tokenHash: String, revokedAt: Instant): Boolean = jdbc.update(
        """
        UPDATE credentials SET revoked_at = :at
        WHERE secret_hash = :hash AND kind = :kind AND revoked_at IS NULL
        """.trimIndent(),
        mapOf("hash" to tokenHash, "kind" to PANEL_SESSION, "at" to revokedAt.toSqlTimestamp()),
    ) == 1

    // --- администратор ---

    override fun administrator(): PanelAdministrator? = jdbc.query(
        "SELECT username, password_hash, created_at, updated_at FROM panel_administrators WHERE singleton",
    ) { rs, _ ->
        PanelAdministrator(
            username = rs.getString("username"),
            passwordHash = rs.getString("password_hash"),
            createdAt = rs.getTimestamp("created_at").toInstant(),
            updatedAt = rs.getTimestamp("updated_at").toInstant(),
        )
    }.firstOrNull()

    /** false — администратор уже заведён; повторная установка не должна его подменять. */
    override fun createAdministrator(administrator: PanelAdministrator): Boolean = jdbc.update(
        """
        INSERT INTO panel_administrators (singleton, username, password_hash, created_at, updated_at)
        VALUES (true, :username, :passwordHash, :createdAt, :updatedAt)
        ON CONFLICT (singleton) DO NOTHING
        """.trimIndent(),
        mapOf(
            "username" to administrator.username, "passwordHash" to administrator.passwordHash,
            "createdAt" to administrator.createdAt.toSqlTimestamp(),
            "updatedAt" to administrator.updatedAt.toSqlTimestamp(),
        ),
    ) == 1

    // --- общее ---

    private fun insert(
        id: String,
        kind: String,
        subject: String,
        secretHash: String,
        role: String,
        createdBy: String,
        createdAt: Instant,
        expiresAt: Instant?,
    ) {
        jdbc.update(
            """
            INSERT INTO credentials (id, kind, subject, secret_hash, role, created_by, created_at, expires_at)
            VALUES (:id, :kind, :subject, :hash, :role, :createdBy, :createdAt, :expiresAt)
            """.trimIndent(),
            mapOf(
                "id" to id, "kind" to kind, "subject" to subject, "hash" to secretHash, "role" to role,
                "createdBy" to createdBy, "createdAt" to createdAt.toSqlTimestamp(),
                "expiresAt" to expiresAt?.toSqlTimestamp(),
            ),
        )
    }

    /** Единственная выборка предъявителя: и для токена, и для сессии. */
    private fun findActive(kind: String, secretHash: String, now: Instant): CredentialRow? = jdbc.query(
        """
        $SELECT
        WHERE secret_hash = :hash AND kind = :kind AND revoked_at IS NULL
          AND (expires_at IS NULL OR expires_at > :now)
        """.trimIndent(),
        mapOf("hash" to secretHash, "kind" to kind, "now" to now.toSqlTimestamp()),
    ) { rs, _ -> row(rs) }.firstOrNull()

    private fun row(rs: ResultSet) = CredentialRow(
        id = rs.getString("id"),
        subject = rs.getString("subject"),
        secretHash = rs.getString("secret_hash"),
        role = rs.getString("role"),
        createdBy = rs.getString("created_by"),
        createdAt = rs.getTimestamp("created_at").toInstant(),
        expiresAt = rs.getTimestamp("expires_at")?.toInstant(),
        lastUsedAt = rs.getTimestamp("last_used_at")?.toInstant(),
        revokedAt = rs.getTimestamp("revoked_at")?.toInstant(),
    )

    private fun touchById(id: String, usedAt: Instant) {
        jdbc.update(
            "UPDATE credentials SET last_used_at = :at WHERE id = :id",
            mapOf("id" to id, "at" to usedAt.toSqlTimestamp()),
        )
    }

    private companion object {
        val SELECT = """
            SELECT id, kind, subject, secret_hash, role, created_by, created_at,
                   expires_at, last_used_at, revoked_at
            FROM credentials
        """.trimIndent()
    }
}

/** Одна строка credentials до того, как её истолковали как токен или сессию. */
private data class CredentialRow(
    val id: String,
    val subject: String,
    val secretHash: String,
    val role: String,
    val createdBy: String,
    val createdAt: Instant,
    val expiresAt: Instant?,
    val lastUsedAt: Instant?,
    val revokedAt: Instant?,
) {
    fun toApiToken() = ApiToken(
        id = id, name = subject, tokenHash = secretHash,
        role = AccessRole.valueOf(role.uppercase()), createdBy = createdBy,
        createdAt = createdAt, expiresAt = expiresAt, revokedAt = revokedAt, lastUsedAt = lastUsedAt,
    )

    fun toPanelSession() = PanelSession(
        id = id, tokenHash = secretHash, username = subject,
        createdAt = createdAt, expiresAt = requireNotNull(expiresAt) { "panel session must expire" },
        lastUsedAt = lastUsedAt, revokedAt = revokedAt,
    )
}
