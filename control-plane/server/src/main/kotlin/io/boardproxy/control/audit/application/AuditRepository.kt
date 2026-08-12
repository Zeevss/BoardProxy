package io.boardproxy.control.audit.application

import io.boardproxy.control.audit.domain.AuditEvent

fun interface AuditRepository {
    fun append(event: AuditEvent)
}
