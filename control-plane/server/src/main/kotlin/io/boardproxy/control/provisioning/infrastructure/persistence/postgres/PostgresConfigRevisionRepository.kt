package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.application.ConfigRevisionRepository
import io.boardproxy.control.provisioning.domain.model.ConfigRevision
import io.boardproxy.control.shared.security.EncryptedSecret
import io.boardproxy.control.shared.security.SecretCipher
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.security.MessageDigest
import java.time.Instant

@Repository
class PostgresConfigRevisionRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val secrets: SecretCipher,
) : ConfigRevisionRepository {
    override fun append(
        nodeId: String,
        catalogVersion: Long,
        configToml: ByteArray,
        cause: String,
        createdAt: Instant,
    ): ConfigRevision {
        val current = latest(nodeId)
        val digest = configToml.sha256()
        if (current?.configSha256 == digest) return current
        val revision = (current?.revision ?: 0) + 1
        val value = ConfigRevision(
            nodeId = nodeId, revision = revision, previousRevision = current?.revision ?: 0,
            catalogVersion = catalogVersion, configToml = configToml.copyOf(),
            configSha256 = digest, cause = cause, createdAt = createdAt,
        )
        val encrypted = secrets.encrypt(configContext(nodeId, revision), configToml.toString(Charsets.UTF_8))
        jdbc.update(
            """
            INSERT INTO desired_config_revisions (
                node_id, revision, previous_revision, catalog_version,
                config_ciphertext, config_nonce, config_key_id,
                config_sha256, cause, created_at
            ) VALUES (
                :nodeId, :revision, :previousRevision, :catalogVersion,
                :ciphertext, :nonce, :keyId, :sha256, :cause, :createdAt
            )
            """.trimIndent(),
            mapOf(
                "nodeId" to value.nodeId, "revision" to value.revision,
                "previousRevision" to value.previousRevision, "catalogVersion" to value.catalogVersion,
                "ciphertext" to encrypted.ciphertext, "nonce" to encrypted.nonce,
                "keyId" to encrypted.keyId, "sha256" to value.configSha256,
                "cause" to value.cause, "createdAt" to value.createdAt.toSqlTimestamp(),
            ),
        )
        return value
    }

    override fun latest(nodeId: String): ConfigRevision? = jdbc.query(
        """
        SELECT node_id, revision, previous_revision, catalog_version,
               config_ciphertext, config_nonce, config_key_id,
               config_sha256, cause, created_at
        FROM desired_config_revisions
        WHERE node_id = :nodeId
        ORDER BY revision DESC
        LIMIT 1
        """.trimIndent(),
        mapOf("nodeId" to nodeId),
    ) { rs, _ ->
        ConfigRevision(
            nodeId = rs.getString("node_id"), revision = rs.getLong("revision"),
            previousRevision = rs.getLong("previous_revision"), catalogVersion = rs.getLong("catalog_version"),
            configToml = secrets.decrypt(
                configContext(rs.getString("node_id"), rs.getLong("revision")),
                EncryptedSecret(
                    rs.getBytes("config_ciphertext"), rs.getBytes("config_nonce"),
                    rs.getString("config_key_id"),
                ),
            ).toByteArray(Charsets.UTF_8),
            configSha256 = rs.getString("config_sha256"),
            cause = rs.getString("cause"), createdAt = rs.getTimestamp("created_at").toInstant(),
        )
    }.firstOrNull()
}

private fun ByteArray.sha256(): String = MessageDigest.getInstance("SHA-256")
    .digest(this)
    .joinToString(separator = "") { "%02x".format(it) }

private fun configContext(nodeId: String, revision: Long) = "desired-state:$nodeId:revision:$revision:config"
