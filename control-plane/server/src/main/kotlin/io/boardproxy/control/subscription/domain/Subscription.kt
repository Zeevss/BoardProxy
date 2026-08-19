package io.boardproxy.control.subscription.domain

import java.time.Instant

enum class SubscriptionState { ENABLED, DISABLED, REVOKED }

/**
 * Подписка указывает на пользователя, а не перечисляет пары «нода — пользователь».
 *
 * Прежняя таблица subscription_keys была копией размещений и могла с ними
 * разойтись: добавление ноды пользователю не создавало в подписке ключ. Теперь
 * набор ключей выводится из грантов при каждом резолве.
 */
data class Subscription(
    val id: String,
    val name: String,
    val userId: String,
    val tokenHash: String,
    val recoveryPublicKey: String,
    val state: SubscriptionState,
    val version: Long,
    val createdAt: Instant,
    val updatedAt: Instant,
) {
    init {
        require(id.matches(ID_PATTERN)) { "invalid subscription id" }
        require(userId.matches(ID_PATTERN)) { "invalid subscription user reference" }
        require(name.isNotBlank()) { "subscription name is required" }
        require(tokenHash.matches(Regex("^[0-9a-f]{64}$"))) { "invalid subscription token hash" }
        require(recoveryPublicKey.matches(Regex("^[A-Za-z0-9_-]{43}$"))) { "invalid recovery public key" }
        require(version > 0) { "subscription version must be positive" }
    }
}

private val ID_PATTERN = Regex("^[A-Za-z0-9._:-]{1,128}$")
