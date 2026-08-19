package io.boardproxy.control.provisioning.api.rest

import io.boardproxy.control.provisioning.application.NodeConfigDiff
import io.boardproxy.control.provisioning.application.NodeHistoryCommands
import io.boardproxy.control.provisioning.application.NodeHistoryQueries
import io.boardproxy.control.provisioning.application.NodeSnapshotMetadata
import io.boardproxy.control.provisioning.application.Page
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController
import java.security.Principal

@RestController
@RequestMapping("/api/v1/nodes/{nodeId}/history")
class NodeHistoryController(
    private val queries: NodeHistoryQueries,
    private val commands: NodeHistoryCommands,
) {
    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun history(
        @PathVariable nodeId: String,
        @RequestParam(defaultValue = "0") offset: Int,
        @RequestParam(defaultValue = "50") limit: Int,
    ): Page<NodeSnapshotMetadata> = queries.history(nodeId, offset, limit.coerceIn(1, MAXIMUM_PAGE))

    @GetMapping("/diff")
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun diff(
        @PathVariable nodeId: String,
        @RequestParam from: Long,
        @RequestParam to: Long,
    ): NodeConfigDiff = queries.diff(nodeId, from, to)

    /** Откат не возвращает ревизию назад: состояние применяется как обычная правка. */
    @PostMapping("/{seq}/rollback")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun rollback(
        @PathVariable nodeId: String,
        @PathVariable seq: Long,
        principal: Principal,
    ) = commands.rollback(nodeId, seq, principal.name)

    private companion object {
        const val MAXIMUM_PAGE = 200
    }
}
