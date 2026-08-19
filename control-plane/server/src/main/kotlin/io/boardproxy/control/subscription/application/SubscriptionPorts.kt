package io.boardproxy.control.subscription.application

import io.boardproxy.control.subscription.domain.Subscription
import io.boardproxy.control.subscription.domain.SubscriptionState

/** Секреты подписки. Читаются отдельным вызовом, чтобы не попадать в списки и обычные чтения. */
data class SubscriptionSecrets(val token: String, val recoveryClientPrivateKey: String)

interface SubscriptionRepository {
    fun create(subscription: Subscription, secrets: SubscriptionSecrets)
    fun replace(subscription: Subscription, expectedVersion: Long): Boolean

    /** Отдельно от replace: смена имени/состояния не должна трогать секреты, и наоборот. */
    fun rotateSecrets(subscription: Subscription, expectedVersion: Long, secrets: SubscriptionSecrets): Boolean

    fun findSecrets(id: String): SubscriptionSecrets?
    fun find(id: String): Subscription?
    fun findByTokenHash(tokenHash: String): Subscription?
    fun findByRecoveryPublicKey(publicKey: String): Subscription?
    fun list(userId: String?, offset: Int, limit: Int): List<Subscription>
    fun count(userId: String?): Long
    fun delete(id: String, expectedVersion: Long): Boolean
}

data class SubscriptionDraft(val name: String, val userId: String)

data class SubscriptionReplacement(val name: String, val state: SubscriptionState)

data class IssuedSubscription(
    val subscription: Subscription,
    val token: String,
    val recoveryClientPrivateKey: String,
)

interface SubscriptionLinkBuilder {
    val enabled: Boolean
    fun build(issued: IssuedSubscription): String
}

interface SubscriptionCommands {
    fun create(draft: SubscriptionDraft, actor: String): IssuedSubscription
    fun replace(id: String, expectedVersion: Long, replacement: SubscriptionReplacement, actor: String): Subscription

    /**
     * Выпускает новый токен и новую recovery-пару той же подписке. Ротация
     * обесценивает прежнюю ссылку — это единственный способ отозвать утёкшую.
     */
    fun rotate(id: String, expectedVersion: Long, actor: String): IssuedSubscription

    fun delete(id: String, expectedVersion: Long, actor: String)
}

interface SubscriptionQueries {
    fun get(id: String): Subscription
    fun list(userId: String?, offset: Int, limit: Int): SubscriptionPage
    fun resolve(token: String?, recoveryPublicKey: String?): SubscriptionSnapshot

    /** Постоянная ссылка подписки; null, если доставка выключена. */
    fun link(id: String): String?
}

data class SubscriptionPage(
    val items: List<Subscription>,
    val offset: Int,
    val limit: Int,
    val total: Long,
)

data class SubscriptionSnapshot(
    val version: Int = 1,
    val id: String,
    val name: String,
    val state: String,
    val revision: String,
    val issuedAt: java.time.Instant,
    val usedBytes: Long,
    val trafficLimit: Long,
    val keys: List<SubscriptionKeySnapshot>,
)

data class SubscriptionKeySnapshot(
    val id: String,
    val name: String,
    val nodeId: String,
    val userId: String,
    val state: String,
    val usedBytes: Long,
    val keylink: String? = null,
)
