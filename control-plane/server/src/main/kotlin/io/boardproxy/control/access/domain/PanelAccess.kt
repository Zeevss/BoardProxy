package io.boardproxy.control.access.domain

import java.time.Instant

data class PanelAdministrator(
    val username: String,
    val passwordHash: String,
    val createdAt: Instant,
    val updatedAt: Instant,
)

data class PanelSession(
    val id: String,
    val tokenHash: String,
    val username: String,
    val createdAt: Instant,
    val expiresAt: Instant,
    val lastUsedAt: Instant? = null,
    val revokedAt: Instant? = null,
)
