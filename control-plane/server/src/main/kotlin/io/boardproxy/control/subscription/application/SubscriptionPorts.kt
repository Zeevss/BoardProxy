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

    /** null, когда подписка выпущена до появления восстановимых секретов. */
    fun findSecrets(id: String): SubscriptionSecrets?
    fun find(id: String): Subscription?
    fun findByTokenHash(tokenHash: String): Subscription?
    fun findByRecoveryPublicKey(publicKey: String): Subscription?
    fun list(): List<Subscription>
}

data class SubscriptionDraft(val name: String, val keys: List<SubscriptionKeyDraft>)
data class SubscriptionKeyDraft(val id: String, val name: String, val nodeId: String, val userId: String)
data class SubscriptionReplacement(
    val name: String,
    val state: SubscriptionState,
    val keys: List<SubscriptionKeyDraft>,
)

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
     * Выпускает новый токен и новую recovery-пару той же подписке. Control-plane
     * хранит только хеш токена, поэтому выданную ссылку нельзя показать повторно —
     * ротация это единственный способ получить рабочую ссылку снова.
     */
    fun rotate(id: String, expectedVersion: Long, actor: String): IssuedSubscription
}

interface SubscriptionQueries {
    fun get(id: String): Subscription
    fun list(): List<Subscription>
    fun resolve(token: String?, recoveryPublicKey: String?): SubscriptionSnapshot

    /** Постоянная ссылка подписки; null, если секреты не сохранены или доставка выключена. */
    fun link(id: String): String?
}

data class SubscriptionSnapshot(
    val version: Int = 1,
    val id: String,
    val name: String,
    val state: String,
    val revision: String,
    val issuedAt: java.time.Instant,
    val usedBytes: Long,
    val trafficLimit: Long = 0,
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
