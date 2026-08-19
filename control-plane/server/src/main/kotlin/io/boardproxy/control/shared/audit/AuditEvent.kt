package io.boardproxy.control.shared.audit

import java.time.Instant

data class AuditEvent(
    val id: String,
    val nodeId: String?,
    val actor: String,
    val action: String,
    val resourceType: String,
    val resourceId: String,
    val resourceVersion: Long,
    val details: Map<String, Any> = emptyMap(),
    val occurredAt: Instant,
)
