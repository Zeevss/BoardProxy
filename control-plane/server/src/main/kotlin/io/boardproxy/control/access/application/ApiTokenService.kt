package io.boardproxy.control.access.application

import io.boardproxy.control.access.domain.AccessPrincipal
import io.boardproxy.control.access.domain.AccessRole
import io.boardproxy.control.access.domain.ApiToken
import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.security.MessageDigest
import java.security.SecureRandom
import java.time.Clock
import java.time.Duration
import java.util.Base64
import java.util.UUID

class ApiTokenService(
    private val tokens: ApiTokenRepository,
    private val audit: AuditRepository,
    private val transactions: TransactionRunner,
    private val clock: Clock,
    private val bootstrapToken: String,
    private val random: SecureRandom = SecureRandom(),
    private val nextId: () -> String = { UUID.randomUUID().toString() },
) : ApiTokenCommands, ApiTokenQueries, AccessAuthenticator {
    override fun list(): List<ApiToken> = tokens.list()

    override fun authenticate(rawToken: String): AccessPrincipal? {
        if (rawToken.isBlank()) return null
        if (bootstrapToken.isNotBlank() && MessageDigest.isEqual(rawToken.sha256Bytes(), bootstrapToken.sha256Bytes())) {
            return AccessPrincipal("bootstrap-admin", AccessRole.ADMIN)
        }
        val token = tokens.findActiveByHash(rawToken.sha256Hex(), clock.instant()) ?: return null
        tokens.touch(token.id, clock.instant())
        return AccessPrincipal(token.name, token.role)
    }

    override fun issue(name: String, role: AccessRole, ttl: Duration?, actor: String): IssuedApiToken {
        val normalizedName = name.trim()
        if (normalizedName.isBlank()) throw InvalidRequest("token name is required")
        if (actor.isBlank()) throw InvalidRequest("actor is required")
        if (ttl != null && (ttl.isNegative || ttl.isZero || ttl > MAX_TTL)) {
            throw InvalidRequest("token ttl must be positive and at most 365 days")
        }
        val now = clock.instant()
        val secret = "bpat_" + Base64.getUrlEncoder().withoutPadding().encodeToString(ByteArray(32).also(random::nextBytes))
        val token = ApiToken(
            id = nextId(), name = normalizedName, tokenHash = secret.sha256Hex(), role = role,
            createdBy = actor, createdAt = now, expiresAt = ttl?.let(now::plus),
        )
        transactions.required {
            tokens.create(token)
            audit.append(token.audit("api-token.created", actor, now))
        }
        return IssuedApiToken(token, secret)
    }

    override fun revoke(id: String, actor: String) {
        if (!revokeIfActive(id, actor)) throw ResourceNotFound("api token $id not found or already revoked")
    }

    override fun revokeIfActive(id: String, actor: String): Boolean {
        if (actor.isBlank()) throw InvalidRequest("actor is required")
        val now = clock.instant()
        return transactions.required {
            if (!tokens.revoke(id, now)) return@required false
            audit.append(
                AuditEvent(
                    id = nextId(), nodeId = null, actor = actor, action = "api-token.revoked",
                    resourceType = "api-token", resourceId = id, resourceVersion = 0,
                    occurredAt = now,
                ),
            )
            true
        }
    }

    private fun ApiToken.audit(action: String, actor: String, now: java.time.Instant) = AuditEvent(
        id = nextId(), nodeId = null, actor = actor, action = action,
        resourceType = "api-token", resourceId = id, resourceVersion = 0,
        details = mapOf("name" to name, "role" to role.name.lowercase()), occurredAt = now,
    )

    private fun String.sha256Bytes(): ByteArray = MessageDigest.getInstance("SHA-256").digest(toByteArray())
    private fun String.sha256Hex(): String = sha256Bytes().joinToString("") { "%02x".format(it) }

    private companion object {
        val MAX_TTL: Duration = Duration.ofDays(365)
    }
}
