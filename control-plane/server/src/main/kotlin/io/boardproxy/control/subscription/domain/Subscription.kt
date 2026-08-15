package io.boardproxy.control.subscription.domain

import java.time.Instant

enum class SubscriptionState { ENABLED, DISABLED, REVOKED }

data class SubscriptionKey(
    val id: String,
    val name: String,
    val nodeId: String,
    val userId: String,
    val position: Int,
) {
    init {
        require(id.matches(ID_PATTERN)) { "invalid subscription key id" }
        require(name.isNotBlank()) { "subscription key name is required" }
        require(nodeId.matches(ID_PATTERN) && userId.matches(ID_PATTERN)) { "invalid subscription key reference" }
        require(position >= 0) { "subscription key position cannot be negative" }
    }
}

data class Subscription(
    val id: String,
    val name: String,
    val tokenHash: String,
    val recoveryPublicKey: String,
    val state: SubscriptionState,
    val keys: List<SubscriptionKey>,
    val version: Long,
    val createdAt: Instant,
    val updatedAt: Instant,
) {
    init {
        require(id.matches(ID_PATTERN)) { "invalid subscription id" }
        require(name.isNotBlank()) { "subscription name is required" }
        require(tokenHash.matches(Regex("^[0-9a-f]{64}$"))) { "invalid subscription token hash" }
        require(recoveryPublicKey.matches(Regex("^[A-Za-z0-9_-]{43}$"))) { "invalid recovery public key" }
        require(version > 0) { "subscription version must be positive" }
        require(keys.isNotEmpty()) { "subscription must contain at least one key" }
        require(keys.map(SubscriptionKey::id).distinct().size == keys.size) { "duplicate subscription key id" }
        require(keys.map { it.nodeId to it.userId }.distinct().size == keys.size) { "duplicate subscription key target" }
        require(keys.map(SubscriptionKey::position).distinct().size == keys.size) { "duplicate subscription key position" }
    }
}

private val ID_PATTERN = Regex("^[A-Za-z0-9._:-]{1,128}$")
