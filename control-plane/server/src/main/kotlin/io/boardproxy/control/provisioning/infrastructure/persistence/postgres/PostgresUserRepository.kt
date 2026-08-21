package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.application.UserRepository
import io.boardproxy.control.provisioning.domain.model.User
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.shared.security.EncryptedSecret
import io.boardproxy.control.shared.security.SecretCipher
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet

private const val COLUMNS =
    """id, name, description, private_key_ciphertext, private_key_nonce, private_key_key_id,
       public_key, state, max_sessions, max_lanes, resource_version, updated_at"""

/**
 * Пользователь флотовый: одна строка на человека и один зашифрованный ключ,
 * вместо копии на каждую ноду. Отбор по ноде идёт через гранты.
 */
@Repository
class PostgresUserRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val secrets: SecretCipher,
) : UserRepository {

    override fun find(id: String): User? = jdbc.query(
        "SELECT $COLUMNS FROM users WHERE id = :id",
        mapOf("id" to id),
        ::row,
    ).firstOrNull()

    override fun findByFingerprint(fingerprint: String): User? = jdbc.query(
        "SELECT $COLUMNS FROM users WHERE identity_fingerprint = :fingerprint",
        mapOf("fingerprint" to fingerprint),
        ::row,
    ).firstOrNull()

    override fun list(query: String?, nodeId: String?, offset: Int, limit: Int): List<User> = jdbc.query(
        """
        SELECT $COLUMNS FROM users
        ${where(query, nodeId)}
        ORDER BY name, id OFFSET :offset LIMIT :limit
        """.trimIndent(),
        filterParameters(query, nodeId) + mapOf("offset" to offset, "limit" to limit),
        ::row,
    )

    override fun count(query: String?, nodeId: String?): Long = jdbc.queryForObject(
        "SELECT count(*) FROM users ${where(query, nodeId)}",
        filterParameters(query, nodeId),
        Long::class.java,
    ) ?: 0

    override fun create(user: User) {
        jdbc.update(
            """
            INSERT INTO users (
                id, name, description, private_key_ciphertext, private_key_nonce, private_key_key_id,
                public_key, identity_fingerprint, state, max_sessions, max_lanes,
                resource_version, updated_at
            ) VALUES (
                :id, :name, :description, :ciphertext, :nonce, :keyId, :publicKey, :fingerprint,
                :state, :maxSessions, :maxLanes, :version, :updatedAt
            )
            """.trimIndent(),
            parameters(user),
        )
    }

    override fun replace(user: User, expectedVersion: Long): Boolean = jdbc.update(
        """
        UPDATE users SET
            name = :name, description = :description, private_key_ciphertext = :ciphertext, private_key_nonce = :nonce,
            private_key_key_id = :keyId, public_key = :publicKey, identity_fingerprint = :fingerprint,
            state = :state, max_sessions = :maxSessions, max_lanes = :maxLanes,
            resource_version = :version, updated_at = :updatedAt
        WHERE id = :id AND resource_version = :expectedVersion
        """.trimIndent(),
        parameters(user) + ("expectedVersion" to expectedVersion),
    ) == 1

    /** Гранты, квота и подписки уходят каскадом. */
    override fun delete(id: String): Boolean =
        jdbc.update("DELETE FROM users WHERE id = :id", mapOf("id" to id)) == 1

    private fun parameters(user: User): Map<String, Any?> {
        val encrypted = user.privateKey?.let { secrets.encrypt(userKeyContext(user.id), it) }
        return mapOf(
            "id" to user.id,
            "name" to user.name,
            "description" to user.description,
            "ciphertext" to encrypted?.ciphertext,
            "nonce" to encrypted?.nonce,
            "keyId" to encrypted?.keyId,
            "publicKey" to user.publicKey,
            "fingerprint" to user.identityFingerprint(),
            "state" to user.state.databaseValue(),
            "maxSessions" to user.maxSessions,
            "maxLanes" to user.maxLanes,
            "version" to user.version,
            "updatedAt" to user.updatedAt.toSqlTimestamp(),
        )
    }

    private fun row(rs: ResultSet, @Suppress("UNUSED_PARAMETER") index: Int): User {
        val ciphertext = rs.getBytes("private_key_ciphertext")
        val privateKey = ciphertext?.let {
            secrets.decrypt(
                userKeyContext(rs.getString("id")),
                EncryptedSecret(it, rs.getBytes("private_key_nonce"), rs.getString("private_key_key_id")),
            )
        }
        return User(
            id = rs.getString("id"),
            name = rs.getString("name"),
            description = rs.getString("description"),
            privateKey = privateKey,
            publicKey = rs.getString("public_key"),
            state = rs.getString("state").resourceState(),
            maxSessions = rs.getInt("max_sessions"),
            maxLanes = rs.getInt("max_lanes"),
            version = rs.getLong("resource_version"),
            updatedAt = rs.getTimestamp("updated_at").toInstant(),
        )
    }

    private fun where(query: String?, nodeId: String?): String {
        val conditions = buildList {
            if (!nodeId.isNullOrBlank()) {
                add("EXISTS (SELECT 1 FROM grants g WHERE g.user_id = users.id AND g.node_id = :nodeId)")
            }
            if (!query.isNullOrBlank()) {
                add(
                    "(id ILIKE '%' || :query || '%' OR name ILIKE '%' || :query || '%' " +
                        "OR description ILIKE '%' || :query || '%')",
                )
            }
        }
        return if (conditions.isEmpty()) "" else "WHERE ${conditions.joinToString(" AND ")}"
    }

    private fun filterParameters(query: String?, nodeId: String?): Map<String, Any?> =
        mapOf("query" to query.orEmpty(), "nodeId" to nodeId.orEmpty())
}
