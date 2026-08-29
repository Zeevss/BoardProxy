package io.boardproxy.control.access.application

import io.boardproxy.control.access.domain.AccessPrincipal
import io.boardproxy.control.access.domain.AccessRole
import io.boardproxy.control.access.domain.ApiToken
import java.time.Duration
import java.time.Instant

interface ApiTokenRepository {
    fun create(token: ApiToken)
    fun findActiveByHash(tokenHash: String, now: Instant): ApiToken?
    fun list(): List<ApiToken>
    fun revoke(id: String, revokedAt: Instant): Boolean
    fun touch(id: String, usedAt: Instant)
}

fun interface AccessAuthenticator {
    fun authenticate(rawToken: String): AccessPrincipal?
}

interface ApiTokenCommands {
    fun issue(name: String, role: AccessRole, ttl: Duration?, actor: String): IssuedApiToken
    fun revoke(id: String, actor: String)
    /** Внутренние владельцы токена могут безопасно ротировать уже отозванный secret. */
    fun revokeIfActive(id: String, actor: String): Boolean
}

fun interface ApiTokenQueries {
    fun list(): List<ApiToken>
}

data class IssuedApiToken(val token: ApiToken, val secret: String)
