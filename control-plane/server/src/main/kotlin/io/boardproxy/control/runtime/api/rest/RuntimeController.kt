package io.boardproxy.control.runtime.api.rest

import io.boardproxy.control.runtime.application.RuntimeQueries
import io.boardproxy.control.runtime.application.RuntimeProjectionRebuild
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceNotFound
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController
import org.springframework.web.bind.annotation.PostMapping

@RestController
@RequestMapping("/api/v1/nodes/{nodeId}/runtime")
@PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
class RuntimeController(
    private val queries: RuntimeQueries,
    private val rebuild: RuntimeProjectionRebuild,
) {
    @GetMapping
    fun projection(@PathVariable nodeId: String): RuntimeProjectionResponse =
        (queries.projection(nodeId) ?: throw ResourceNotFound("node $nodeId runtime projection not found"))
            .toResponse()

    @GetMapping("/events")
    fun events(
        @PathVariable nodeId: String,
        @RequestParam(required = false) coreBootId: String?,
        @RequestParam(required = false) afterSequence: Long?,
        @RequestParam(defaultValue = "100") limit: Int,
    ): List<RuntimeEventResponse> {
        if (limit !in 1..MAX_LIMIT) throw InvalidRequest("runtime event limit must be between 1 and $MAX_LIMIT")
        if (afterSequence != null && (afterSequence < 0 || coreBootId.isNullOrBlank())) {
            throw InvalidRequest("afterSequence requires a non-blank coreBootId and must be non-negative")
        }
        if (coreBootId != null && coreBootId.isBlank()) throw InvalidRequest("coreBootId must not be blank")
        return queries.events(nodeId, coreBootId, afterSequence, limit).map { event -> event.toResponse() }
    }

    @PostMapping("/rebuild")
    @PreAuthorize("hasRole('ADMIN')")
    fun rebuild(@PathVariable nodeId: String): RuntimeProjectionResponse = rebuild.rebuild(nodeId).toResponse()

    private companion object {
        const val MAX_LIMIT = 500
    }
}
