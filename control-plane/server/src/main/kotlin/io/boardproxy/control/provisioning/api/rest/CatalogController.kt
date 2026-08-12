package io.boardproxy.control.provisioning.api.rest

import io.boardproxy.control.provisioning.application.CatalogCommands
import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import org.springframework.http.HttpStatus
import org.springframework.http.ResponseEntity
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.PutMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestHeader
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import java.security.Principal
import java.time.Clock

@RestController
@RequestMapping("/api/v1/catalogs")
class CatalogController(
    private val commands: CatalogCommands,
    private val queries: CatalogQueries,
    private val clock: Clock,
) {
    @PostMapping
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun create(
        @RequestBody request: CatalogWriteRequest,
        principal: Principal,
    ): ResponseEntity<CatalogMutationResponse> {
        val result = commands.create(request.toNewCatalog(clock.instant()), principal.name)
        return ResponseEntity.status(HttpStatus.CREATED)
            .eTag(result.catalog.version.toString())
            .body(result.toResponse())
    }

    @GetMapping("/{nodeId}")
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun get(@PathVariable nodeId: String): ResponseEntity<CatalogResponse> {
        val catalog = queries.get(nodeId)
        return ResponseEntity.ok().eTag(catalog.version.toString()).body(catalog.toResponse())
    }

    @PutMapping("/{nodeId}")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun replace(
        @PathVariable nodeId: String,
        @RequestHeader("If-Match") ifMatch: String,
        @RequestBody request: CatalogWriteRequest,
        principal: Principal,
    ): ResponseEntity<CatalogMutationResponse> {
        if (request.node.id != nodeId) throw InvalidRequest("path nodeId must match request node.id")
        val expectedVersion = ifMatch.removeSurrounding("\"").toLongOrNull()
            ?: throw InvalidRequest("If-Match must contain the numeric catalog version")
        val current = queries.get(nodeId)
        if (current.version != expectedVersion) {
            throw ResourceConflict("catalog $nodeId version changed")
        }
        val candidate = request.toReplacement(clock.instant(), current)
        val result = commands.replace(candidate, expectedVersion, principal.name)
        return ResponseEntity.ok().eTag(result.catalog.version.toString()).body(result.toResponse())
    }
}

internal fun io.boardproxy.control.provisioning.application.CatalogMutationResult.toResponse() = CatalogMutationResponse(
    catalog = catalog.toResponse(),
    desiredRevision = desiredRevision.revision,
    configSha256 = desiredRevision.configSha256,
    configChanged = configChanged,
)
