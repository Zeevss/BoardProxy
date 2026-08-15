package io.boardproxy.control.subscription.infrastructure.persistence.postgres

import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.subscription.application.SubscriptionRepository
import io.boardproxy.control.subscription.domain.Subscription
import io.boardproxy.control.subscription.domain.SubscriptionKey
import io.boardproxy.control.subscription.domain.SubscriptionState
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet

@Repository
class PostgresSubscriptionRepository(private val jdbc: NamedParameterJdbcTemplate) : SubscriptionRepository {
    override fun create(subscription: Subscription) {
        jdbc.update(
            """
            INSERT INTO subscriptions (
                id, name, token_hash, recovery_public_key, state, resource_version, created_at, updated_at
            ) VALUES (
                :id, :name, :tokenHash, :recoveryPublicKey, :state, :version, :createdAt, :updatedAt
            )
            """.trimIndent(),
            parameters(subscription),
        )
        insertKeys(subscription)
    }

    override fun replace(subscription: Subscription, expectedVersion: Long): Boolean {
        val updated = jdbc.update(
            """
            UPDATE subscriptions SET
                name = :name, state = :state, resource_version = :version, updated_at = :updatedAt
            WHERE id = :id AND resource_version = :expectedVersion
            """.trimIndent(),
            parameters(subscription) + ("expectedVersion" to expectedVersion),
        )
        if (updated != 1) return false
        jdbc.update(
            "DELETE FROM subscription_keys WHERE subscription_id = :id",
            mapOf("id" to subscription.id),
        )
        insertKeys(subscription)
        return true
    }

    override fun find(id: String): Subscription? = findOne("id = :value", id)

    override fun findByTokenHash(tokenHash: String): Subscription? = findOne("token_hash = :value", tokenHash)

    override fun findByRecoveryPublicKey(publicKey: String): Subscription? =
        findOne("recovery_public_key = :value", publicKey)

    override fun list(): List<Subscription> = jdbc.query(
        "$SELECT ORDER BY created_at DESC",
        emptyMap<String, Any>(),
    ) { rs, _ -> row(rs) }.map(::hydrate)

    private fun findOne(predicate: String, value: String): Subscription? = jdbc.query(
        "$SELECT WHERE $predicate",
        mapOf("value" to value),
    ) { rs, _ -> row(rs) }.firstOrNull()?.let(::hydrate)

    private fun hydrate(row: SubscriptionRow) = Subscription(
        id = row.id, name = row.name, tokenHash = row.tokenHash,
        recoveryPublicKey = row.recoveryPublicKey, state = row.state,
        keys = jdbc.query(
            """
            SELECT key_id, display_name, node_id, user_id, position
            FROM subscription_keys WHERE subscription_id = :id ORDER BY position
            """.trimIndent(),
            mapOf("id" to row.id),
        ) { rs, _ ->
            SubscriptionKey(
                rs.getString("key_id"), rs.getString("display_name"),
                rs.getString("node_id"), rs.getString("user_id"), rs.getInt("position"),
            )
        },
        version = row.version, createdAt = row.createdAt, updatedAt = row.updatedAt,
    )

    private fun insertKeys(subscription: Subscription) {
        jdbc.batchUpdate(
            """
            INSERT INTO subscription_keys (
                subscription_id, key_id, display_name, node_id, user_id, position
            ) VALUES (
                :subscriptionId, :keyId, :displayName, :nodeId, :userId, :position
            )
            """.trimIndent(),
            subscription.keys.map { key ->
                mapOf(
                    "subscriptionId" to subscription.id, "keyId" to key.id,
                    "displayName" to key.name, "nodeId" to key.nodeId,
                    "userId" to key.userId, "position" to key.position,
                )
            }.toTypedArray(),
        )
    }

    private fun parameters(subscription: Subscription): Map<String, Any> = mapOf(
        "id" to subscription.id, "name" to subscription.name,
        "tokenHash" to subscription.tokenHash, "recoveryPublicKey" to subscription.recoveryPublicKey,
        "state" to subscription.state.name.lowercase(), "version" to subscription.version,
        "createdAt" to subscription.createdAt.toSqlTimestamp(), "updatedAt" to subscription.updatedAt.toSqlTimestamp(),
    )

    private fun row(rs: ResultSet) = SubscriptionRow(
        id = rs.getString("id"), name = rs.getString("name"), tokenHash = rs.getString("token_hash"),
        recoveryPublicKey = rs.getString("recovery_public_key"),
        state = SubscriptionState.valueOf(rs.getString("state").uppercase()),
        version = rs.getLong("resource_version"),
        createdAt = rs.getTimestamp("created_at").toInstant(), updatedAt = rs.getTimestamp("updated_at").toInstant(),
    )

    private data class SubscriptionRow(
        val id: String,
        val name: String,
        val tokenHash: String,
        val recoveryPublicKey: String,
        val state: SubscriptionState,
        val version: Long,
        val createdAt: java.time.Instant,
        val updatedAt: java.time.Instant,
    )

    private companion object {
        val SELECT = """
            SELECT id, name, token_hash, recovery_public_key, state, resource_version, created_at, updated_at
            FROM subscriptions
        """.trimIndent()
    }
}
