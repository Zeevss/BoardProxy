package io.boardproxy.control.fleet.infrastructure.persistence.postgres

import io.boardproxy.control.fleet.application.NodeCertificateRepository
import io.boardproxy.control.fleet.domain.IssuedCertificate
import io.boardproxy.control.fleet.domain.NodeCertificate
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.security.MessageDigest
import java.security.cert.CertificateFactory
import java.time.Instant

@Repository
class PostgresNodeCertificateRepository(
    private val jdbc: NamedParameterJdbcTemplate,
) : NodeCertificateRepository {
    override fun record(nodeId: String, issued: IssuedCertificate) {
        val certificate = CertificateFactory.getInstance("X.509")
            .generateCertificate(issued.certificatePem.inputStream())
        val fingerprint = MessageDigest.getInstance("SHA-256").digest(certificate.encoded).hex()
        jdbc.update(
            """
            INSERT INTO node_certificates (
                serial_number, node_id, certificate_pem, fingerprint_sha256, issued_at, expires_at
            ) VALUES (:serial, :nodeId, :pem, :fingerprint, :issuedAt, :expiresAt)
            ON CONFLICT (serial_number) DO NOTHING
            """.trimIndent(),
            mapOf(
                "serial" to issued.serialNumber, "nodeId" to nodeId,
                "pem" to issued.certificatePem.toString(Charsets.UTF_8), "fingerprint" to fingerprint,
                "issuedAt" to issued.issuedAt.toSqlTimestamp(), "expiresAt" to issued.expiresAt.toSqlTimestamp(),
            ),
        )
    }

    override fun list(nodeId: String): List<NodeCertificate> = jdbc.query(
        """
        SELECT serial_number, node_id, fingerprint_sha256, issued_at, expires_at,
               revoked_at, revoked_reason, last_seen_at
        FROM node_certificates WHERE node_id = :nodeId ORDER BY issued_at DESC
        """.trimIndent(),
        mapOf("nodeId" to nodeId),
    ) { rs, _ ->
        NodeCertificate(
            rs.getString("serial_number"), rs.getString("node_id"), rs.getString("fingerprint_sha256"),
            rs.getTimestamp("issued_at").toInstant(), rs.getTimestamp("expires_at").toInstant(),
            rs.getTimestamp("revoked_at")?.toInstant(), rs.getString("revoked_reason"),
            rs.getTimestamp("last_seen_at")?.toInstant(),
        )
    }

    override fun revoke(
        nodeId: String,
        serialNumber: String,
        reason: String,
        revokedAt: Instant,
    ): Boolean = jdbc.update(
        """
        UPDATE node_certificates SET revoked_at = :revokedAt, revoked_reason = :reason
        WHERE node_id = :nodeId AND serial_number = :serial AND revoked_at IS NULL
        """.trimIndent(),
        mapOf(
            "nodeId" to nodeId, "serial" to serialNumber, "reason" to reason,
            "revokedAt" to revokedAt.toSqlTimestamp(),
        ),
    ) == 1

    override fun active(nodeId: String, serialNumber: String, now: Instant): Boolean = requireNotNull(
        jdbc.queryForObject(
            """
            SELECT EXISTS(
                SELECT 1 FROM node_certificates
                WHERE node_id = :nodeId AND serial_number = :serial
                  AND revoked_at IS NULL AND expires_at > :now
            )
            """.trimIndent(),
            mapOf("nodeId" to nodeId, "serial" to serialNumber, "now" to now.toSqlTimestamp()),
            Boolean::class.java,
        ),
    )

    override fun touch(nodeId: String, serialNumber: String, seenAt: Instant) {
        jdbc.update(
            """
            UPDATE node_certificates SET last_seen_at = :seenAt
            WHERE node_id = :nodeId AND serial_number = :serial
              AND (last_seen_at IS NULL OR last_seen_at < :threshold)
            """.trimIndent(),
            mapOf(
                "nodeId" to nodeId, "serial" to serialNumber, "seenAt" to seenAt.toSqlTimestamp(),
                "threshold" to seenAt.minusSeconds(300).toSqlTimestamp(),
            ),
        )
    }

    override fun nodeEnabled(nodeId: String): Boolean = requireNotNull(
        jdbc.queryForObject(
            "SELECT EXISTS(SELECT 1 FROM nodes WHERE id = :nodeId AND state = 'enabled')",
            mapOf("nodeId" to nodeId),
            Boolean::class.java,
        ),
    )

    private fun ByteArray.hex() = joinToString("") { "%02x".format(it) }
}
