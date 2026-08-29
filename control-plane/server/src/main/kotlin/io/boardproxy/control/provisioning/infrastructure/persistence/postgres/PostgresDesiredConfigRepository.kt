package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.application.DesiredConfig
import io.boardproxy.control.provisioning.application.DesiredConfigRepository
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.shared.security.EncryptedSecret
import io.boardproxy.control.shared.security.SecretCipher
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository

/**
 * Хранится только текущая конфигурация: одна строка на ноду.
 *
 * Истории скомпилированных TOML нет намеренно — её никто не читал. Откат
 * применяет снимок исходного состояния и пересобирает конфигурацию заново,
 * поэтому производную форму достаточно держать в одном экземпляре.
 */
@Repository
class PostgresDesiredConfigRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val secrets: SecretCipher,
) : DesiredConfigRepository {

    override fun lock(nodeId: String) {
        val locked = jdbc.queryForList(
            "SELECT id FROM nodes WHERE id = :nodeId FOR UPDATE",
            mapOf("nodeId" to nodeId),
            String::class.java,
        )
        require(locked.size == 1) { "node $nodeId does not exist" }
    }

    override fun find(nodeId: String): DesiredConfig? = jdbc.query(
        """
        SELECT node_id, revision, config_sha256, config_ciphertext, config_nonce,
               config_key_id, updated_at
        FROM node_desired_config WHERE node_id = :nodeId
        """.trimIndent(),
        mapOf("nodeId" to nodeId),
    ) { rs, _ ->
        val toml = secrets.decrypt(
            configContext(rs.getString("node_id")),
            EncryptedSecret(
                rs.getBytes("config_ciphertext"),
                rs.getBytes("config_nonce"),
                rs.getString("config_key_id"),
            ),
        )
        DesiredConfig(
            nodeId = rs.getString("node_id"),
            revision = rs.getLong("revision"),
            configSha256 = rs.getString("config_sha256"),
            configToml = toml.toByteArray(Charsets.UTF_8),
            updatedAt = rs.getTimestamp("updated_at").toInstant(),
        )
    }.firstOrNull()

    override fun save(config: DesiredConfig) {
        val encrypted = secrets.encrypt(
            configContext(config.nodeId),
            config.configToml.toString(Charsets.UTF_8),
        )
        jdbc.update(
            """
            INSERT INTO node_desired_config (
                node_id, revision, config_ciphertext, config_nonce, config_key_id,
                config_sha256, updated_at
            ) VALUES (
                :nodeId, :revision, :ciphertext, :nonce, :keyId, :sha256, :updatedAt
            )
            ON CONFLICT (node_id) DO UPDATE SET
                revision = EXCLUDED.revision,
                config_ciphertext = EXCLUDED.config_ciphertext,
                config_nonce = EXCLUDED.config_nonce,
                config_key_id = EXCLUDED.config_key_id,
                config_sha256 = EXCLUDED.config_sha256,
                updated_at = EXCLUDED.updated_at
            """.trimIndent(),
            mapOf(
                "nodeId" to config.nodeId,
                "revision" to config.revision,
                "ciphertext" to encrypted.ciphertext,
                "nonce" to encrypted.nonce,
                "keyId" to encrypted.keyId,
                "sha256" to config.configSha256,
                "updatedAt" to config.updatedAt.toSqlTimestamp(),
            ),
        )
    }
}
