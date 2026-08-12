package io.boardproxy.control.fleet.infrastructure.persistence.postgres

import io.boardproxy.control.fleet.application.EnrollmentTokenRepository
import io.boardproxy.control.fleet.domain.EnrollmentToken
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.security.MessageDigest
import java.security.SecureRandom
import java.time.Clock
import java.time.Duration
import java.util.Base64

@Repository
class PostgresEnrollmentTokenRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val clock: Clock,
) : EnrollmentTokenRepository {
    private val random = SecureRandom()

    override fun create(nodeId: String, ttl: Duration): EnrollmentToken {
        val plaintext = Base64.getUrlEncoder().withoutPadding().encodeToString(ByteArray(32).also(random::nextBytes))
        val expiresAt = clock.instant().plus(ttl)
        jdbc.update(
            """
            INSERT INTO enrollment_tokens (token_hash, node_id, expires_at)
            VALUES (:hash, :nodeId, :expiresAt)
            """.trimIndent(),
            mapOf(
                "hash" to plaintext.sha256(), "nodeId" to nodeId,
                "expiresAt" to expiresAt.toSqlTimestamp(),
            ),
        )
        return EnrollmentToken(plaintext, expiresAt)
    }

    override fun consume(nodeId: String, plaintext: String): Boolean = jdbc.update(
        """
        UPDATE enrollment_tokens SET consumed_at = :now
        WHERE token_hash = :hash AND node_id = :nodeId
          AND consumed_at IS NULL AND expires_at > :now
        """.trimIndent(),
        mapOf(
            "hash" to plaintext.sha256(), "nodeId" to nodeId,
            "now" to clock.instant().toSqlTimestamp(),
        ),
    ) == 1
}

private fun String.sha256(): String = MessageDigest.getInstance("SHA-256")
    .digest(toByteArray(Charsets.UTF_8))
    .joinToString("") { "%02x".format(it) }
