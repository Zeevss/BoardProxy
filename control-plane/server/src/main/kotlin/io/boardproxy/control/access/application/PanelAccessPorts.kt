package io.boardproxy.control.access.application

import io.boardproxy.control.access.domain.PanelAdministrator
import io.boardproxy.control.access.domain.PanelSession
import java.time.Instant

interface PanelAccessRepository {
    fun administrator(): PanelAdministrator?
    fun createAdministrator(administrator: PanelAdministrator): Boolean
    fun createSession(session: PanelSession)
    fun findActiveSessionByHash(tokenHash: String, now: Instant): PanelSession?
    fun touchSession(id: String, usedAt: Instant)
    fun revokeSessionByHash(tokenHash: String, revokedAt: Instant): Boolean
}

interface PasswordHasher {
    fun hash(password: String): String
    fun matches(password: String, encoded: String): Boolean
}

data class PanelAuthStatus(val setupRequired: Boolean)
data class IssuedPanelSession(val token: String, val username: String, val expiresAt: Instant)

interface PanelAuthOperations {
    fun status(): PanelAuthStatus
    fun setup(username: String, password: String): IssuedPanelSession
    fun login(username: String, password: String): IssuedPanelSession
    fun logout(rawToken: String)
}
