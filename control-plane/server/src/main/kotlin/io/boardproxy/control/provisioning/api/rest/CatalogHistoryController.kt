package io.boardproxy.control.provisioning.api.rest

import io.boardproxy.control.provisioning.application.CatalogDiff
import io.boardproxy.control.provisioning.application.CatalogHistoryCommands
import io.boardproxy.control.provisioning.application.CatalogHistoryPage
import io.boardproxy.control.provisioning.application.CatalogHistoryQueries
import io.boardproxy.control.provisioning.application.CatalogOverviewQueries
import io.boardproxy.control.provisioning.application.CatalogPage
import io.boardproxy.control.shared.errors.InvalidRequest
import org.springframework.http.ResponseEntity
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestHeader
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController
import java.security.Principal

@RestController
@RequestMapping("/api/v1/nodes")
class CatalogHistoryController(
    private val overview: CatalogOverviewQueries,
    private val history: CatalogHistoryQueries,
    private val commands: CatalogHistoryCommands,
) {
    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun nodes(
        @RequestParam(required = false) query: String?,
        @RequestParam(defaultValue = "0") offset: Int,
        @RequestParam(defaultValue = "50") limit: Int,
    ): CatalogPage {
        page(offset, limit)
        return overview.search(query, offset, limit)
    }

    @GetMapping("/{nodeId}/revisions")
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun revisions(
        @PathVariable nodeId: String,
        @RequestParam(defaultValue = "0") offset: Int,
        @RequestParam(defaultValue = "50") limit: Int,
    ): CatalogHistoryPage {
        page(offset, limit)
        return history.history(nodeId, offset, limit)
    }

    @GetMapping("/{nodeId}/revisions/diff")
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun diff(
        @PathVariable nodeId: String,
        @RequestParam from: Long,
        @RequestParam to: Long,
    ): CatalogDiff = history.diff(nodeId, from, to)

    @PostMapping("/{nodeId}/revisions/{catalogVersion}/rollback")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun rollback(
        @PathVariable nodeId: String,
        @PathVariable catalogVersion: Long,
        @RequestHeader("If-Match") ifMatch: String,
        principal: Principal,
    ): ResponseEntity<CatalogMutationResponse> {
        val expected = ifMatch.removeSurrounding("\"").toLongOrNull()
            ?: throw InvalidRequest("If-Match must contain the numeric catalog version")
        val result = commands.rollback(nodeId, catalogVersion, expected, principal.name)
        return ResponseEntity.ok().eTag(result.catalog.version.toString()).body(result.toResponse())
    }

    private fun page(offset: Int, limit: Int) {
        if (offset < 0 || limit !in 1..200) throw InvalidRequest("offset must be non-negative and limit between 1 and 200")
    }
}
