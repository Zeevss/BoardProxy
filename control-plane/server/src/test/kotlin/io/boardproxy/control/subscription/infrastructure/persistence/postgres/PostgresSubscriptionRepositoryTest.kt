package io.boardproxy.control.subscription.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresUserRepository
import io.boardproxy.control.subscription.application.SubscriptionSecrets
import io.boardproxy.control.subscription.domain.Subscription
import io.boardproxy.control.subscription.domain.SubscriptionState
import io.boardproxy.control.testing.PostgresSupport
import io.boardproxy.control.testing.TEST_TIME
import io.boardproxy.control.testing.testUser
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class PostgresSubscriptionRepositoryTest {
    private val users = PostgresUserRepository(PostgresSupport.named, PostgresSupport.cipher)
    private val subscriptions = PostgresSubscriptionRepository(PostgresSupport.named, PostgresSupport.cipher)

    @BeforeTest
    fun prepare() {
        assertTrue(PostgresSupport.dockerAvailable, "тесты репозиториев требуют Docker")
        PostgresSupport.truncate()
        users.create(testUser("user-1"))
        users.create(testUser("user-2", keyByte = 3))
    }

    @Test
    fun `подписка переживает круг записи и чтения`() {
        val subscription = subscription()
        subscriptions.create(subscription, secrets())

        assertEquals(subscription, subscriptions.find("sub-1"))
        assertEquals(subscription, subscriptions.findByTokenHash(subscription.tokenHash))
        assertEquals(subscription, subscriptions.findByRecoveryPublicKey(subscription.recoveryPublicKey))
    }

    /** Постоянная ссылка восстановима, поэтому секреты хранятся зашифрованными, а не только хешем. */
    @Test
    fun `секреты восстанавливаются и не лежат открытым текстом`() {
        subscriptions.create(subscription(), secrets())

        val restored = assertNotNull(subscriptions.findSecrets("sub-1"))
        assertEquals("bps_secret-token", restored.token)
        assertEquals("recovery-private", restored.recoveryClientPrivateKey)

        val ciphertext = PostgresSupport.jdbc.queryForObject(
            "SELECT encode(token_ciphertext, 'escape') FROM subscriptions WHERE id = 'sub-1'",
            String::class.java,
        )
        assertFalse(ciphertext!!.contains("bps_secret-token"))
    }

    @Test
    fun `замена не трогает секреты, ротация их меняет`() {
        subscriptions.create(subscription(), secrets())

        assertTrue(
            subscriptions.replace(subscription(name = "Renamed", version = 2), expectedVersion = 1),
        )
        assertEquals("bps_secret-token", subscriptions.findSecrets("sub-1")?.token)

        val rotated = subscription(version = 3, tokenHash = "b".repeat(64), recoveryPublicKey = "B".repeat(43))
        assertTrue(
            subscriptions.rotateSecrets(rotated, expectedVersion = 2, SubscriptionSecrets("bps_new", "recovery-new")),
        )
        assertEquals("bps_new", subscriptions.findSecrets("sub-1")?.token)
        assertNull(subscriptions.findByTokenHash("a".repeat(64)), "прежний токен обесценен")
    }

    @Test
    fun `замена и удаление защищены версией`() {
        subscriptions.create(subscription(), secrets())

        assertFalse(subscriptions.replace(subscription(version = 2), expectedVersion = 99))
        assertFalse(subscriptions.delete("sub-1", expectedVersion = 99))
        assertTrue(subscriptions.delete("sub-1", expectedVersion = 1))
        assertNull(subscriptions.find("sub-1"))
    }

    @Test
    fun `список фильтруется по пользователю и считается`() {
        subscriptions.create(subscription("sub-1", userId = "user-1"), secrets())
        subscriptions.create(
            subscription("sub-2", userId = "user-2", tokenHash = "b".repeat(64), recoveryPublicKey = "B".repeat(43)),
            SubscriptionSecrets("bps_other", "recovery-other"),
        )

        assertEquals(2, subscriptions.count(null))
        assertEquals(1, subscriptions.count("user-1"))
        assertEquals(listOf("sub-1"), subscriptions.list("user-1", 0, 10).map { it.id })
    }

    /** Подписка привязана к пользователю: удаление человека уносит и его подписки. */
    @Test
    fun `удаление пользователя уносит подписку`() {
        subscriptions.create(subscription(), secrets())

        users.delete("user-1")

        assertNull(subscriptions.find("sub-1"))
    }

    private fun subscription(
        id: String = "sub-1",
        userId: String = "user-1",
        name: String = "Алиса",
        version: Long = 1,
        tokenHash: String = "a".repeat(64),
        recoveryPublicKey: String = "A".repeat(43),
    ) = Subscription(
        id = id, name = name, userId = userId, tokenHash = tokenHash,
        recoveryPublicKey = recoveryPublicKey, state = SubscriptionState.ENABLED,
        version = version, createdAt = TEST_TIME, updatedAt = TEST_TIME,
    )

    private fun secrets() = SubscriptionSecrets("bps_secret-token", "recovery-private")
}
