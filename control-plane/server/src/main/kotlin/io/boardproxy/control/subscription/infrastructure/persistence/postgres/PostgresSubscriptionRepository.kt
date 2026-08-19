package io.boardproxy.control.subscription.infrastructure.persistence.postgres

import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.shared.security.EncryptedSecret
import io.boardproxy.control.shared.security.SecretCipher
import io.boardproxy.control.subscription.application.SubscriptionRepository
import io.boardproxy.control.subscription.application.SubscriptionSecrets
import io.boardproxy.control.subscription.domain.Subscription
import io.boardproxy.control.subscription.domain.SubscriptionState
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet

/**
 * Таблицы subscription_keys больше нет: подписка ссылается на пользователя, а
 * набор ключей выводится из его грантов при резолве.
 */
@Repository
class PostgresSubscriptionRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val cipher: SecretCipher,
) : SubscriptionRepository {

    override fun create(subscription: Subscription, secrets: SubscriptionSecrets) {
        jdbc.update(
            """
            INSERT INTO subscriptions (
                id, name, user_id, token_hash, recovery_public_key, state,
                resource_version, created_at, updated_at,
                token_ciphertext, token_nonce, token_key_id,
                recovery_private_ciphertext, recovery_private_nonce, recovery_private_key_id
            ) VALUES (
                :id, :name, :userId, :tokenHash, :recoveryPublicKey, :state,
                :version, :createdAt, :updatedAt,
                :tokenCiphertext, :tokenNonce, :tokenKeyId,
                :recoveryCiphertext, :recoveryNonce, :recoveryKeyId
            )
            """.trimIndent(),
            parameters(subscription) + secretParameters(subscription.id, secrets),
        )
    }

    override fun replace(subscription: Subscription, expectedVersion: Long): Boolean = jdbc.update(
        """
        UPDATE subscriptions SET
            name = :name, state = :state, resource_version = :version, updated_at = :updatedAt
        WHERE id = :id AND resource_version = :expectedVersion
        """.trimIndent(),
        parameters(subscription) + ("expectedVersion" to expectedVersion),
    ) == 1

    override fun rotateSecrets(
        subscription: Subscription,
        expectedVersion: Long,
        secrets: SubscriptionSecrets,
    ): Boolean = jdbc.update(
        """
        UPDATE subscriptions SET
            token_hash = :tokenHash, recovery_public_key = :recoveryPublicKey,
            token_ciphertext = :tokenCiphertext, token_nonce = :tokenNonce, token_key_id = :tokenKeyId,
            recovery_private_ciphertext = :recoveryCiphertext, recovery_private_nonce = :recoveryNonce,
            recovery_private_key_id = :recoveryKeyId,
            resource_version = :version, updated_at = :updatedAt
        WHERE id = :id AND resource_version = :expectedVersion
        """.trimIndent(),
        parameters(subscription) + secretParameters(subscription.id, secrets) + ("expectedVersion" to expectedVersion),
    ) == 1

    override fun delete(id: String, expectedVersion: Long): Boolean = jdbc.update(
        "DELETE FROM subscriptions WHERE id = :id AND resource_version = :expectedVersion",
        mapOf("id" to id, "expectedVersion" to expectedVersion),
    ) == 1

    override fun findSecrets(id: String): SubscriptionSecrets? = jdbc.query(
        """
        SELECT token_ciphertext, token_nonce, token_key_id,
               recovery_private_ciphertext, recovery_private_nonce, recovery_private_key_id
        FROM subscriptions WHERE id = :id
        """.trimIndent(),
        mapOf("id" to id),
    ) { rs, _ ->
        SubscriptionSecrets(
            token = cipher.decrypt(
                tokenContext(id),
                EncryptedSecret(
                    rs.getBytes("token_ciphertext"), rs.getBytes("token_nonce"), rs.getString("token_key_id"),
                ),
            ),
            recoveryClientPrivateKey = cipher.decrypt(
                recoveryContext(id),
                EncryptedSecret(
                    rs.getBytes("recovery_private_ciphertext"),
                    rs.getBytes("recovery_private_nonce"),
                    rs.getString("recovery_private_key_id"),
                ),
            ),
        )
    }.firstOrNull()

    override fun find(id: String): Subscription? = findOne("id = :value", id)

    override fun findByTokenHash(tokenHash: String): Subscription? = findOne("token_hash = :value", tokenHash)

    override fun findByRecoveryPublicKey(publicKey: String): Subscription? =
        findOne("recovery_public_key = :value", publicKey)

    override fun list(userId: String?, offset: Int, limit: Int): List<Subscription> = jdbc.query(
        """
        $SELECT ${filter(userId)}
        ORDER BY created_at DESC OFFSET :offset LIMIT :limit
        """.trimIndent(),
        mapOf("userId" to userId.orEmpty(), "offset" to offset, "limit" to limit),
    ) { rs, _ -> row(rs) }

    override fun count(userId: String?): Long = jdbc.queryForObject(
        "SELECT count(*) FROM subscriptions ${filter(userId)}",
        mapOf("userId" to userId.orEmpty()),
        Long::class.java,
    ) ?: 0

    private fun filter(userId: String?): String = if (userId.isNullOrBlank()) "" else "WHERE user_id = :userId"

    private fun findOne(predicate: String, value: String): Subscription? = jdbc.query(
        "$SELECT WHERE $predicate",
        mapOf("value" to value),
    ) { rs, _ -> row(rs) }.firstOrNull()

    private fun secretParameters(id: String, secrets: SubscriptionSecrets): Map<String, Any?> {
        val token = cipher.encrypt(tokenContext(id), secrets.token)
        val recovery = cipher.encrypt(recoveryContext(id), secrets.recoveryClientPrivateKey)
        return mapOf(
            "tokenCiphertext" to token.ciphertext, "tokenNonce" to token.nonce, "tokenKeyId" to token.keyId,
            "recoveryCiphertext" to recovery.ciphertext, "recoveryNonce" to recovery.nonce,
            "recoveryKeyId" to recovery.keyId,
        )
    }

    // Контекст входит в AAD, поэтому шифртекст нельзя переставить между подписками или полями.
    private fun tokenContext(id: String) = "subscription:$id:token"
    private fun recoveryContext(id: String) = "subscription:$id:recovery"

    private fun parameters(subscription: Subscription): Map<String, Any?> = mapOf(
        "id" to subscription.id, "name" to subscription.name, "userId" to subscription.userId,
        "tokenHash" to subscription.tokenHash, "recoveryPublicKey" to subscription.recoveryPublicKey,
        "state" to subscription.state.name.lowercase(), "version" to subscription.version,
        "createdAt" to subscription.createdAt.toSqlTimestamp(),
        "updatedAt" to subscription.updatedAt.toSqlTimestamp(),
    )

    private fun row(rs: ResultSet) = Subscription(
        id = rs.getString("id"),
        name = rs.getString("name"),
        userId = rs.getString("user_id"),
        tokenHash = rs.getString("token_hash"),
        recoveryPublicKey = rs.getString("recovery_public_key"),
        state = SubscriptionState.valueOf(rs.getString("state").uppercase()),
        version = rs.getLong("resource_version"),
        createdAt = rs.getTimestamp("created_at").toInstant(),
        updatedAt = rs.getTimestamp("updated_at").toInstant(),
    )

    private companion object {
        val SELECT = """
            SELECT id, name, user_id, token_hash, recovery_public_key, state,
                   resource_version, created_at, updated_at
            FROM subscriptions
        """.trimIndent()
    }
}
