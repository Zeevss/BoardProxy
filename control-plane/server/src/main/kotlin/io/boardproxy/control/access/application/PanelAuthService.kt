package io.boardproxy.control.access.application

import io.boardproxy.control.access.domain.AccessPrincipal
import io.boardproxy.control.access.domain.AccessRole
import io.boardproxy.control.access.domain.PanelAdministrator
import io.boardproxy.control.access.domain.PanelSession
import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.audit.domain.AuditEvent
import io.boardproxy.control.shared.errors.AuthenticationFailed
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.security.MessageDigest
import java.security.SecureRandom
import java.time.Clock
import java.time.Duration
import java.time.Instant
import java.util.Base64
import java.util.UUID

class PanelAuthService(
    private val repository: PanelAccessRepository,
    private val passwords: PasswordHasher,
    private val audit: AuditRepository,
    private val transactions: TransactionRunner,
    private val clock: Clock,
    private val sessionTtl: Duration,
    private val random: SecureRandom = SecureRandom(),
    private val nextId: () -> String = { UUID.randomUUID().toString() },
) : PanelAuthOperations, AccessAuthenticator {
    private val dummyPasswordHash = passwords.hash("unused-${nextId()}")

    init {
        require(!sessionTtl.isNegative && !sessionTtl.isZero) { "panel session ttl must be positive" }
    }

    override fun status() = PanelAuthStatus(setupRequired = repository.administrator() == null)

    override fun setup(username: String, password: String): IssuedPanelSession {
        val normalized = validateUsername(username)
        validatePassword(password)
        val now = clock.instant()
        return transactions.required {
            val created = repository.createAdministrator(
                PanelAdministrator(normalized, passwords.hash(password), now, now),
            )
            if (!created) throw ResourceConflict("panel administrator is already configured")
            audit.append(event("panel-administrator.created", normalized, "panel-administrator", normalized, now))
            issueSession(normalized, now)
        }
    }

    override fun login(username: String, password: String): IssuedPanelSession {
        val normalized = username.trim().lowercase()
        val administrator = repository.administrator()
        val passwordMatches = passwords.matches(password, administrator?.passwordHash ?: dummyPasswordHash)
        if (
            normalized.isBlank() || administrator == null || administrator.username != normalized ||
            !passwordMatches
        ) {
            throw AuthenticationFailed("invalid username or password")
        }
        val now = clock.instant()
        return transactions.required {
            val issued = issueSession(administrator.username, now)
            audit.append(event("panel-session.created", administrator.username, "panel-session", issued.username, now))
            issued
        }
    }

    override fun authenticate(rawToken: String): AccessPrincipal? {
        if (!rawToken.startsWith(SESSION_PREFIX)) return null
        val session = repository.findActiveSessionByHash(rawToken.sha256Hex(), clock.instant()) ?: return null
        repository.touchSession(session.id, clock.instant())
        return AccessPrincipal(session.username, AccessRole.ADMIN)
    }

    override fun logout(rawToken: String) {
        if (!rawToken.startsWith(SESSION_PREFIX)) return
        val now = clock.instant()
        repository.revokeSessionByHash(rawToken.sha256Hex(), now)
    }

    private fun issueSession(username: String, now: Instant): IssuedPanelSession {
        val secret = SESSION_PREFIX + Base64.getUrlEncoder().withoutPadding()
            .encodeToString(ByteArray(32).also(random::nextBytes))
        val expiresAt = now.plus(sessionTtl)
        repository.createSession(
            PanelSession(nextId(), secret.sha256Hex(), username, now, expiresAt),
        )
        return IssuedPanelSession(secret, username, expiresAt)
    }

    private fun validateUsername(value: String): String {
        val normalized = value.trim().lowercase()
        if (!USERNAME.matches(normalized)) {
            throw InvalidRequest("username must be 3-64 characters and contain only letters, digits, dot, dash or underscore")
        }
        return normalized
    }

    private fun validatePassword(value: String) {
        if (value.length !in MIN_PASSWORD_LENGTH..MAX_PASSWORD_LENGTH) {
            throw InvalidRequest("password must be between $MIN_PASSWORD_LENGTH and $MAX_PASSWORD_LENGTH characters")
        }
    }

    private fun event(action: String, actor: String, resourceType: String, resourceId: String, now: Instant) = AuditEvent(
        id = nextId(), nodeId = null, actor = actor, action = action,
        resourceType = resourceType, resourceId = resourceId,
        resourceVersion = 0, catalogVersion = 0, occurredAt = now,
    )

    private fun String.sha256Hex(): String = MessageDigest.getInstance("SHA-256")
        .digest(toByteArray(Charsets.UTF_8)).joinToString("") { "%02x".format(it) }

    private companion object {
        const val SESSION_PREFIX = "bps_"
        const val MIN_PASSWORD_LENGTH = 10
        const val MAX_PASSWORD_LENGTH = 256
        val USERNAME = Regex("[a-z0-9][a-z0-9._-]{2,63}")
    }
}
