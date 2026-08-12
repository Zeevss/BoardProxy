package io.boardproxy.control.audit.domain

import java.time.Instant

data class AuditEvent(
    val id: String,
    val nodeId: String?,
    val actor: String,
    val action: String,
    val resourceType: String,
    val resourceId: String,
    val resourceVersion: Long,
    val catalogVersion: Long,
    val details: Map<String, Any> = emptyMap(),
    val occurredAt: Instant,
)
