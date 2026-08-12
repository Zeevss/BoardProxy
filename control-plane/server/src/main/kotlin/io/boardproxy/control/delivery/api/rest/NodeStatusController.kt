package io.boardproxy.control.delivery.api.rest

import io.boardproxy.control.delivery.application.NodeStatusRepository
import io.boardproxy.control.delivery.domain.NodeStatus
import io.boardproxy.control.shared.errors.ResourceNotFound
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController

@RestController
@RequestMapping("/api/v1/nodes")
class NodeStatusController(private val statuses: NodeStatusRepository) {
    @GetMapping("/{nodeId}/status")
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun get(@PathVariable nodeId: String): NodeStatus =
        statuses.find(nodeId) ?: throw ResourceNotFound("node $nodeId status not found")
}
