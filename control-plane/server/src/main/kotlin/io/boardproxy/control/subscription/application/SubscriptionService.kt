package io.boardproxy.control.subscription.application

import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.shared.contracts.KeylinkQueries
import io.boardproxy.control.shared.contracts.UserUsageQueries
import io.boardproxy.control.shared.crypto.X25519
import io.boardproxy.control.shared.crypto.base64Url
import io.boardproxy.control.shared.crypto.sha256Hex
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceForbidden
import io.boardproxy.control.shared.errors.ResourceGone
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.subscription.domain.Subscription
import io.boardproxy.control.subscription.domain.SubscriptionState
import java.security.SecureRandom
import java.time.Clock
import java.time.Instant
import java.util.UUID

class SubscriptionService(
    private val subscriptions: SubscriptionRepository,
    private val keylinks: KeylinkQueries,
    private val usage: UserUsageQueries,
    private val audit: AuditRepository,
    private val transactions: TransactionRunner,
    private val links: SubscriptionLinkBuilder,
    private val clock: Clock,
    private val random: SecureRandom = SecureRandom(),
    private val nextId: () -> String = { UUID.randomUUID().toString() },
) : SubscriptionCommands, SubscriptionQueries {

    override fun create(draft: SubscriptionDraft, actor: String): IssuedSubscription {
        validateActor(actor)
        val now = clock.instant()
        val token = "bps_" + randomBytes(32).base64Url()
        val recoveryPrivate = X25519.generatePrivateKey(random)
        val subscription = Subscription(
            id = nextId(), name = draft.name.trim(), userId = draft.userId.trim(),
            tokenHash = token.sha256Hex(),
            recoveryPublicKey = X25519.publicKeyOf(recoveryPrivate).base64Url(),
            state = SubscriptionState.ENABLED, version = 1, createdAt = now, updatedAt = now,
        )
        val secrets = SubscriptionSecrets(token, recoveryPrivate.base64Url())
        transactions.required {
            subscriptions.create(subscription, secrets)
            audit.append(subscription.audit("subscription.created", actor, now))
        }
        return IssuedSubscription(subscription, token, recoveryPrivate.base64Url())
    }

    override fun replace(
        id: String,
        expectedVersion: Long,
        replacement: SubscriptionReplacement,
        actor: String,
    ): Subscription {
        validateActor(actor)
        if (expectedVersion <= 0) throw InvalidRequest("If-Match subscription version is required")
        val current = subscriptions.find(id) ?: throw ResourceNotFound("subscription $id not found")
        if (current.state == SubscriptionState.REVOKED && replacement.state != SubscriptionState.REVOKED) {
            throw ResourceConflict("revoked subscription $id cannot be re-enabled")
        }
        val now = clock.instant()
        val updated = current.copy(
            name = replacement.name.trim(), state = replacement.state,
            version = expectedVersion + 1, updatedAt = now,
        )
        transactions.required {
            if (!subscriptions.replace(updated, expectedVersion)) {
                throw ResourceConflict("subscription $id version changed")
            }
            audit.append(updated.audit("subscription.replaced", actor, now))
        }
        return updated
    }

    override fun rotate(id: String, expectedVersion: Long, actor: String): IssuedSubscription {
        validateActor(actor)
        if (expectedVersion <= 0) throw InvalidRequest("If-Match subscription version is required")
        val current = subscriptions.find(id) ?: throw ResourceNotFound("subscription $id not found")
        // Отозванная подписка не должна снова стать рабочей через ротацию.
        if (current.state == SubscriptionState.REVOKED) throw ResourceGone("subscription $id is revoked")
        val now = clock.instant()
        val token = "bps_" + randomBytes(32).base64Url()
        val recoveryPrivate = X25519.generatePrivateKey(random)
        val rotated = current.copy(
            tokenHash = token.sha256Hex(),
            recoveryPublicKey = X25519.publicKeyOf(recoveryPrivate).base64Url(),
            version = expectedVersion + 1, updatedAt = now,
        )
        val secrets = SubscriptionSecrets(token, recoveryPrivate.base64Url())
        transactions.required {
            if (!subscriptions.rotateSecrets(rotated, expectedVersion, secrets)) {
                throw ResourceConflict("subscription $id version changed")
            }
            audit.append(rotated.audit("subscription.rotated", actor, now))
        }
        return IssuedSubscription(rotated, token, recoveryPrivate.base64Url())
    }

    override fun delete(id: String, expectedVersion: Long, actor: String) {
        validateActor(actor)
        val current = subscriptions.find(id) ?: throw ResourceNotFound("subscription $id not found")
        val now = clock.instant()
        transactions.required {
            if (!subscriptions.delete(id, expectedVersion)) {
                throw ResourceConflict("subscription $id version changed")
            }
            audit.append(current.audit("subscription.deleted", actor, now))
        }
    }

    override fun get(id: String): Subscription =
        subscriptions.find(id) ?: throw ResourceNotFound("subscription $id not found")

    override fun list(userId: String?, offset: Int, limit: Int): SubscriptionPage = SubscriptionPage(
        items = subscriptions.list(userId, offset, limit),
        offset = offset, limit = limit, total = subscriptions.count(userId),
    )

    override fun link(id: String): String? {
        val subscription = get(id)
        if (!links.enabled) return null
        val secrets = subscriptions.findSecrets(id) ?: return null
        return links.build(IssuedSubscription(subscription, secrets.token, secrets.recoveryClientPrivateKey))
    }

    /**
     * Набор ключей выводится из грантов пользователя, а не хранится в подписке:
     * выданный доступ и содержимое подписки не могут разойтись.
     */
    override fun resolve(token: String?, recoveryPublicKey: String?): SubscriptionSnapshot {
        if (!links.enabled) throw ResourceForbidden("subscription service is disabled")
        if (token.isNullOrBlank() == recoveryPublicKey.isNullOrBlank()) {
            throw InvalidRequest("provide exactly one of token or recoveryPublicKey")
        }
        val subscription = if (!token.isNullOrBlank()) {
            subscriptions.findByTokenHash(token.sha256Hex())
        } else {
            subscriptions.findByRecoveryPublicKey(requireNotNull(recoveryPublicKey))
        } ?: throw ResourceNotFound("subscription not found")
        when (subscription.state) {
            SubscriptionState.DISABLED -> throw ResourceForbidden("subscription is disabled")
            SubscriptionState.REVOKED -> throw ResourceGone("subscription is revoked")
            SubscriptionState.ENABLED -> Unit
        }

        val now = clock.instant()
        // Один индексированный запрос на пользователя вместо выборки тоталов
        // всех пользователей каждой ноды, как было раньше.
        val consumption = usage.usage(subscription.userId)
        val keys = keylinks.forUser(subscription.userId, subscription.name).map { key ->
            SubscriptionKeySnapshot(
                id = key.nodeId,
                name = key.nodeName,
                nodeId = key.nodeId,
                userId = subscription.userId,
                state = if (key.keylink == null) "disabled" else "enabled",
                usedBytes = consumption.perNode[key.nodeId] ?: 0,
                keylink = key.keylink,
            )
        }

        val revisionMaterial = buildString {
            append(subscription.id).append(':').append(subscription.version)
            keys.forEach { key ->
                append('|').append(key.nodeId).append(':').append(key.state)
                    .append(':').append(key.keylink.orEmpty())
            }
        }
        return SubscriptionSnapshot(
            id = subscription.id, name = subscription.name, state = "enabled",
            revision = revisionMaterial.sha256Hex(), issuedAt = now,
            usedBytes = consumption.usedBytes,
            trafficLimit = consumption.limitBytes,
            keys = keys,
        )
    }

    private fun validateActor(actor: String) {
        if (actor.isBlank()) throw InvalidRequest("actor is required")
    }

    private fun randomBytes(size: Int) = ByteArray(size).also(random::nextBytes)

    private fun Subscription.audit(action: String, actor: String, now: Instant) = AuditEvent(
        id = nextId(), nodeId = null, actor = actor, action = action,
        resourceType = "subscription", resourceId = id, resourceVersion = version,
        details = mapOf("name" to name, "userId" to userId, "state" to state.name.lowercase()),
        occurredAt = now,
    )
}
