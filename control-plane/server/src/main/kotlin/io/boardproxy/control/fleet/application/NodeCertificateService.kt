package io.boardproxy.control.fleet.application

import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.audit.domain.AuditEvent
import io.boardproxy.control.fleet.domain.NodeCertificate
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Clock
import java.util.UUID

class NodeCertificateService(
    private val certificates: NodeCertificateRepository,
    private val audit: AuditRepository,
    private val transactions: TransactionRunner,
    private val clock: Clock,
    private val nextId: () -> String = { UUID.randomUUID().toString() },
) : NodeCertificateQueries, NodeCertificateCommands {
    override fun list(nodeId: String): List<NodeCertificate> = certificates.list(nodeId)

    override fun revoke(nodeId: String, serialNumber: String, reason: String, actor: String) {
        if (reason.isBlank()) throw InvalidRequest("certificate revocation reason is required")
        if (actor.isBlank()) throw InvalidRequest("actor is required")
        val now = clock.instant()
        transactions.required {
            if (!certificates.revoke(nodeId, serialNumber, reason.trim(), now)) {
                throw ResourceNotFound("active certificate not found")
            }
            audit.append(
                AuditEvent(
                    nextId(), nodeId, actor, "node.certificate.revoked", "node-certificate", serialNumber,
                    resourceVersion = 0, catalogVersion = 0,
                    details = mapOf("reason" to reason.trim()), occurredAt = now,
                ),
            )
        }
    }
}
