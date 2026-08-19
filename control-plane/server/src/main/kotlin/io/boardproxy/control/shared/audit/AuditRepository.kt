package io.boardproxy.control.shared.audit

import io.boardproxy.control.shared.audit.AuditEvent

fun interface AuditRepository {
    fun append(event: AuditEvent)
}
