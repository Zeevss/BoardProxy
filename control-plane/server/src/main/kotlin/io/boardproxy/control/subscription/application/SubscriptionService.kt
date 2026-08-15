package io.boardproxy.control.subscription.application

import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.audit.domain.AuditEvent
import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.provisioning.domain.model.keylinkFor
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceForbidden
import io.boardproxy.control.shared.errors.ResourceGone
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.subscription.domain.Subscription
import io.boardproxy.control.subscription.domain.SubscriptionKey
import io.boardproxy.control.subscription.domain.SubscriptionState
import io.boardproxy.control.telemetry.application.TrafficQueries
import java.math.BigInteger
import java.security.KeyFactory
import java.security.MessageDigest
import java.security.SecureRandom
import java.security.spec.NamedParameterSpec
import java.security.spec.XECPrivateKeySpec
import java.security.spec.XECPublicKeySpec
import java.time.Clock
import java.time.Instant
import java.util.Base64
import java.util.UUID
import javax.crypto.KeyAgreement

class SubscriptionService(
    private val subscriptions: SubscriptionRepository,
    private val catalogs: CatalogQueries,
    private val traffic: TrafficQueries,
    private val audit: AuditRepository,
    private val transactions: TransactionRunner,
    private val clock: Clock,
    private val random: SecureRandom = SecureRandom(),
    private val nextId: () -> String = { UUID.randomUUID().toString() },
) : SubscriptionCommands, SubscriptionQueries {
    override fun create(draft: SubscriptionDraft, actor: String): IssuedSubscription {
        validateActor(actor)
        val keys = keys(draft.keys)
        validateTargets(keys)
        val now = clock.instant()
        val token = "bps_" + randomBytes(32).base64Url()
        val recoveryPrivate = randomBytes(32)
        val subscription = Subscription(
            id = nextId(), name = draft.name.trim(), tokenHash = token.sha256Hex(),
            recoveryPublicKey = x25519Public(recoveryPrivate).base64Url(),
            state = SubscriptionState.ENABLED, keys = keys, version = 1,
            createdAt = now, updatedAt = now,
        )
        transactions.required {
            subscriptions.create(subscription)
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
        val keys = keys(replacement.keys)
        validateTargets(keys)
        val now = clock.instant()
        val updated = current.copy(
            name = replacement.name.trim(), state = replacement.state, keys = keys,
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

    override fun get(id: String): Subscription =
        subscriptions.find(id) ?: throw ResourceNotFound("subscription $id not found")

    override fun list(): List<Subscription> = subscriptions.list()

    override fun resolve(token: String?, recoveryPublicKey: String?): SubscriptionSnapshot {
        if ((token.isNullOrBlank()) == (recoveryPublicKey.isNullOrBlank())) {
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
        val catalogByNode = subscription.keys.map(SubscriptionKey::nodeId).distinct().associateWith(catalogs::get)
        val trafficByNode = catalogByNode.keys.associateWith { nodeId ->
            traffic.userTotals(nodeId, Instant.EPOCH, now).associateBy { it.subject }
        }
        val snapshots = subscription.keys.sortedBy(SubscriptionKey::position).map { reference ->
            val catalog = catalogByNode.getValue(reference.nodeId)
            val keylink = catalog.keylinkFor(reference.userId, reference.name)
            val total = trafficByNode[reference.nodeId]?.get(reference.userId)
            val used = positiveSum(total?.rxBytes ?: 0, total?.txBytes ?: 0)
            SubscriptionKeySnapshot(
                id = reference.id, name = reference.name, nodeId = reference.nodeId,
                userId = reference.userId, state = if (keylink == null) "disabled" else "enabled",
                usedBytes = used, keylink = keylink,
            )
        }
        val revisionMaterial = buildString {
            append(subscription.id).append(':').append(subscription.version)
            snapshots.forEach { key ->
                val catalog = catalogByNode.getValue(key.nodeId)
                append('|').append(key.id).append(':').append(catalog.version)
                    .append(':').append(key.state).append(':').append(key.keylink.orEmpty())
            }
        }
        return SubscriptionSnapshot(
            id = subscription.id, name = subscription.name, state = "enabled",
            revision = revisionMaterial.sha256Hex(), issuedAt = now,
            usedBytes = snapshots.fold(0L) { sum, key -> positiveSum(sum, key.usedBytes) },
            keys = snapshots,
        )
    }

    private fun keys(drafts: List<SubscriptionKeyDraft>): List<SubscriptionKey> {
        if (drafts.isEmpty()) throw InvalidRequest("subscription must contain at least one key")
        return try {
            drafts.mapIndexed { position, key ->
                SubscriptionKey(key.id.trim(), key.name.trim(), key.nodeId.trim(), key.userId.trim(), position)
            }
        } catch (error: IllegalArgumentException) {
            throw InvalidRequest(error.message ?: "invalid subscription key")
        }
    }

    private fun validateTargets(keys: List<SubscriptionKey>) {
        keys.groupBy(SubscriptionKey::nodeId).forEach { (nodeId, references) ->
            val catalog = catalogs.get(nodeId)
            val users = catalog.users.map { it.id }.toSet()
            references.firstOrNull { it.userId !in users }?.let {
                throw InvalidRequest("user ${it.userId} does not exist in catalog $nodeId")
            }
        }
    }

    private fun validateActor(actor: String) {
        if (actor.isBlank()) throw InvalidRequest("actor is required")
    }

    private fun randomBytes(size: Int) = ByteArray(size).also(random::nextBytes)

    private fun Subscription.audit(action: String, actor: String, now: Instant) = AuditEvent(
        id = nextId(), nodeId = null, actor = actor, action = action,
        resourceType = "subscription", resourceId = id, resourceVersion = version,
        catalogVersion = 0, details = mapOf("name" to name, "keys" to keys.size, "state" to state.name.lowercase()),
        occurredAt = now,
    )
}

private fun x25519Public(privateKey: ByteArray): ByteArray {
    val parameters = NamedParameterSpec.X25519
    val decodedPrivate = KeyFactory.getInstance("X25519")
        .generatePrivate(XECPrivateKeySpec(parameters, privateKey))
    val basePoint = KeyFactory.getInstance("X25519")
        .generatePublic(XECPublicKeySpec(parameters, BigInteger.valueOf(9)))
    return KeyAgreement.getInstance("X25519").run {
        init(decodedPrivate)
        doPhase(basePoint, true)
        generateSecret()
    }
}

private fun ByteArray.base64Url(): String = Base64.getUrlEncoder().withoutPadding().encodeToString(this)
private fun String.sha256Hex(): String = MessageDigest.getInstance("SHA-256")
    .digest(toByteArray()).joinToString("") { "%02x".format(it) }

private fun positiveSum(left: Long, right: Long): Long {
    val safeLeft = left.coerceAtLeast(0)
    val safeRight = right.coerceAtLeast(0)
    return if (Long.MAX_VALUE - safeLeft < safeRight) Long.MAX_VALUE else safeLeft + safeRight
}
