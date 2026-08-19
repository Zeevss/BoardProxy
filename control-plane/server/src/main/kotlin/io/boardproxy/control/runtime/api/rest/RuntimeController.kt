package io.boardproxy.control.runtime.api.rest

import io.boardproxy.control.provisioning.application.Page
import io.boardproxy.control.runtime.application.RuntimeEventView
import io.boardproxy.control.runtime.application.RuntimeQueries
import io.boardproxy.control.runtime.application.RuntimeSnapshotView
import io.boardproxy.control.shared.errors.ResourceNotFound
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController

/**
 * Только чтение. Эндпоинта перестроения проекции больше нет — перестраивать
 * нечего: снимок присылает нода, а журнал ничего не проецирует.
 */
@RestController
@RequestMapping("/api/v1/nodes/{nodeId}/runtime")
@PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
class RuntimeController(private val queries: RuntimeQueries) {

    @GetMapping
    fun snapshot(@PathVariable nodeId: String): RuntimeSnapshotView =
        queries.snapshot(nodeId) ?: throw ResourceNotFound("node $nodeId has not reported runtime state yet")

    @GetMapping("/events")
    fun events(
        @PathVariable nodeId: String,
        @RequestParam(defaultValue = "0") offset: Int,
        @RequestParam(defaultValue = "100") limit: Int,
    ): Page<RuntimeEventView> {
        val bounded = limit.coerceIn(1, MAXIMUM_PAGE)
        return Page(queries.events(nodeId, offset, bounded), offset, bounded, queries.countEvents(nodeId))
    }

    private companion object {
        const val MAXIMUM_PAGE = 500
    }
}
