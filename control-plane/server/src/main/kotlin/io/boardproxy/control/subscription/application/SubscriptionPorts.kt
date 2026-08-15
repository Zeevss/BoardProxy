package io.boardproxy.control.subscription.application

import io.boardproxy.control.subscription.domain.Subscription
import io.boardproxy.control.subscription.domain.SubscriptionState

interface SubscriptionRepository {
    fun create(subscription: Subscription)
    fun replace(subscription: Subscription, expectedVersion: Long): Boolean
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
}

interface SubscriptionQueries {
    fun get(id: String): Subscription
    fun list(): List<Subscription>
    fun resolve(token: String?, recoveryPublicKey: String?): SubscriptionSnapshot
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
