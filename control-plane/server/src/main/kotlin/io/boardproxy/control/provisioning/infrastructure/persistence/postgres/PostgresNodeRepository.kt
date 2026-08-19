package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.provisioning.application.NodeRepository
import io.boardproxy.control.provisioning.domain.model.Node
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.shared.security.EncryptedSecret
import io.boardproxy.control.shared.security.SecretCipher
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet

private const val COLUMNS =
    """id, name, state, core_settings::text AS core_settings, server_key_ciphertext,
       server_key_nonce, server_key_key_id, resource_version, updated_at"""

@Repository
class PostgresNodeRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
    private val secrets: SecretCipher,
) : NodeRepository {

    override fun find(id: String): Node? = jdbc.query(
        "SELECT $COLUMNS FROM nodes WHERE id = :id",
        mapOf("id" to id),
        ::row,
    ).firstOrNull()

    override fun list(query: String?, offset: Int, limit: Int): List<Node> = jdbc.query(
        """
        SELECT $COLUMNS FROM nodes
        ${filter(query)}
        ORDER BY name, id OFFSET :offset LIMIT :limit
        """.trimIndent(),
        mapOf("query" to query.orEmpty(), "offset" to offset, "limit" to limit),
        ::row,
    )

    override fun count(query: String?): Long = jdbc.queryForObject(
        "SELECT count(*) FROM nodes ${filter(query)}",
        mapOf("query" to query.orEmpty()),
        Long::class.java,
    ) ?: 0

    /**
     * Нода — частный случай агента, поэтому строка в agents создаётся вместе с
     * ней: на неё ссылаются статус, команды и отчёты.
     */
    override fun create(node: Node) {
        jdbc.update(
            "INSERT INTO agents (id, kind, name) VALUES (:id, 'node', :name)",
            mapOf("id" to node.id, "name" to node.name),
        )
        jdbc.update(
            """
            INSERT INTO nodes (
                id, name, state, core_settings, server_key_ciphertext, server_key_nonce,
                server_key_key_id, resource_version, updated_at
            ) VALUES (
                :id, :name, :state, CAST(:core AS jsonb), :ciphertext, :nonce,
                :keyId, :version, :updatedAt
            )
            """.trimIndent(),
            parameters(node),
        )
    }

    override fun replace(node: Node, expectedVersion: Long): Boolean {
        val updated = jdbc.update(
            """
            UPDATE nodes SET
                name = :name, state = :state, core_settings = CAST(:core AS jsonb),
                server_key_ciphertext = :ciphertext, server_key_nonce = :nonce,
                server_key_key_id = :keyId, resource_version = :version, updated_at = :updatedAt
            WHERE id = :id AND resource_version = :expectedVersion
            """.trimIndent(),
            parameters(node) + ("expectedVersion" to expectedVersion),
        )
        if (updated == 1) {
            jdbc.update("UPDATE agents SET name = :name WHERE id = :id", mapOf("id" to node.id, "name" to node.name))
        }
        return updated == 1
    }

    /** Удаляется строка агента: борды, гранты, конфигурация и телеметрия уходят каскадом. */
    override fun delete(id: String): Boolean =
        jdbc.update("DELETE FROM agents WHERE id = :id", mapOf("id" to id)) == 1

    private fun parameters(node: Node): Map<String, Any?> {
        val key = secrets.encrypt(serverKeyContext(node.id), node.core.server.privateKey)
        return mapOf(
            "id" to node.id,
            "name" to node.name,
            "state" to node.state.databaseValue(),
            "core" to json.writeValueAsString(StoredCoreSettings.from(node.core)),
            "ciphertext" to key.ciphertext,
            "nonce" to key.nonce,
            "keyId" to key.keyId,
            "version" to node.version,
            "updatedAt" to node.updatedAt.toSqlTimestamp(),
        )
    }

    private fun row(rs: ResultSet, @Suppress("UNUSED_PARAMETER") index: Int): Node {
        val stored = json.readValue(rs.getString("core_settings"), StoredCoreSettings::class.java)
        val key = secrets.decrypt(
            serverKeyContext(rs.getString("id")),
            EncryptedSecret(
                rs.getBytes("server_key_ciphertext"),
                rs.getBytes("server_key_nonce"),
                rs.getString("server_key_key_id"),
            ),
        )
        return Node(
            id = rs.getString("id"),
            name = rs.getString("name"),
            state = rs.getString("state").resourceState(),
            core = stored.toDomain(key),
            version = rs.getLong("resource_version"),
            updatedAt = rs.getTimestamp("updated_at").toInstant(),
        )
    }

    private fun filter(query: String?): String =
        if (query.isNullOrBlank()) "" else "WHERE id ILIKE '%' || :query || '%' OR name ILIKE '%' || :query || '%'"
}
