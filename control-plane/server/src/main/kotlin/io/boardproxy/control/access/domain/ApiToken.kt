package io.boardproxy.control.access.domain

import java.time.Instant

data class ApiToken(
    val id: String,
    val name: String,
    val tokenHash: String,
    val role: AccessRole,
    val createdBy: String,
    val createdAt: Instant,
    val expiresAt: Instant?,
    val revokedAt: Instant? = null,
    val lastUsedAt: Instant? = null,
)
