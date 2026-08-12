package io.boardproxy.control.fleet.api.rest

import io.boardproxy.control.fleet.application.NodeCertificateCommands
import io.boardproxy.control.fleet.application.NodeCertificateQueries
import io.boardproxy.control.fleet.domain.NodeCertificate
import org.springframework.http.ResponseEntity
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.DeleteMapping
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import java.security.Principal

@RestController
@RequestMapping("/api/v1/nodes/{nodeId}/certificates")
class NodeCertificateController(
    private val queries: NodeCertificateQueries,
    private val commands: NodeCertificateCommands,
) {
    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun list(@PathVariable nodeId: String): List<NodeCertificate> = queries.list(nodeId)

    @DeleteMapping("/{serialNumber}")
    @PreAuthorize("hasRole('ADMIN')")
    fun revoke(
        @PathVariable nodeId: String,
        @PathVariable serialNumber: String,
        @RequestBody request: RevokeCertificateRequest,
        principal: Principal,
    ): ResponseEntity<Void> {
        commands.revoke(nodeId, serialNumber, request.reason, principal.name)
        return ResponseEntity.noContent().build()
    }
}

data class RevokeCertificateRequest(val reason: String)
