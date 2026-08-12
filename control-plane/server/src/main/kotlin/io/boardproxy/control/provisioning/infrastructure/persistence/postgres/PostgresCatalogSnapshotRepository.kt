package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.provisioning.application.CatalogSnapshotMetadata
import io.boardproxy.control.provisioning.application.CatalogSnapshotRepository
import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.shared.security.EncryptedSecret
import io.boardproxy.control.shared.security.SecretCipher
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository

@Repository
class PostgresCatalogSnapshotRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
    private val secrets: SecretCipher,
) : CatalogSnapshotRepository {
    override fun save(catalog: Catalog) {
        val encrypted = secrets.encrypt(context(catalog.node.id, catalog.version), json.writeValueAsString(catalog))
        jdbc.update(
            """
            INSERT INTO catalog_snapshots (
                node_id, catalog_version, payload_ciphertext, payload_nonce,
                payload_key_id, created_at
            ) VALUES (
                :nodeId, :version, :ciphertext, :nonce, :keyId, :createdAt
            ) ON CONFLICT (node_id, catalog_version) DO NOTHING
            """.trimIndent(),
            mapOf(
                "nodeId" to catalog.node.id,
                "version" to catalog.version,
                "ciphertext" to encrypted.ciphertext,
                "nonce" to encrypted.nonce,
                "keyId" to encrypted.keyId,
                "createdAt" to catalog.updatedAt.toSqlTimestamp(),
            ),
        )
    }

    override fun find(nodeId: String, catalogVersion: Long): Catalog? = jdbc.query(
        """
        SELECT payload_ciphertext, payload_nonce, payload_key_id
        FROM catalog_snapshots
        WHERE node_id = :nodeId AND catalog_version = :version
        """.trimIndent(),
        mapOf("nodeId" to nodeId, "version" to catalogVersion),
    ) { rs, _ ->
        val plaintext = secrets.decrypt(
            context(nodeId, catalogVersion),
            EncryptedSecret(
                rs.getBytes("payload_ciphertext"),
                rs.getBytes("payload_nonce"),
                rs.getString("payload_key_id"),
            ),
        )
        json.readValue(plaintext, Catalog::class.java)
    }.firstOrNull()

    override fun list(nodeId: String, offset: Int, limit: Int): List<CatalogSnapshotMetadata> = jdbc.query(
        """
        SELECT node_id, catalog_version, created_at
        FROM catalog_snapshots
        WHERE node_id = :nodeId
        ORDER BY catalog_version DESC
        OFFSET :offset LIMIT :limit
        """.trimIndent(),
        mapOf("nodeId" to nodeId, "offset" to offset, "limit" to limit),
    ) { rs, _ ->
        CatalogSnapshotMetadata(
            rs.getString("node_id"), rs.getLong("catalog_version"), rs.getTimestamp("created_at").toInstant(),
        )
    }

    override fun count(nodeId: String): Long = requireNotNull(
        jdbc.queryForObject(
            "SELECT COUNT(*) FROM catalog_snapshots WHERE node_id = :nodeId",
            mapOf("nodeId" to nodeId),
            Long::class.java,
        ),
    )

    private fun context(nodeId: String, version: Long) = "catalog:$nodeId:snapshot:$version"
}
