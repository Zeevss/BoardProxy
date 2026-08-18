package io.boardproxy.control.provisioning.api.rest

import io.boardproxy.control.provisioning.application.FleetBoard
import io.boardproxy.control.provisioning.application.FleetBoardQueries
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController
import java.time.Instant

/** Борды всего флота: панель больше не привязана к выбранной ноде. */
@RestController
@RequestMapping("/api/v1/boards")
class BoardDirectoryController(private val queries: FleetBoardQueries) {
    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun list(@RequestParam(required = false) query: String?): List<FleetBoardResponse> =
        queries.list(query).map(FleetBoard::toResponse)
}

data class FleetBoardResponse(
    val nodeId: String,
    val nodeName: String,
    val nodeState: String,
    val id: String,
    val name: String,
    val hash: String,
    val hubSlide: String?,
    val apiBase: String?,
    val guestName: String?,
    val state: String,
    val maxLanes: Int,
    val assigned: Boolean,
    val users: Int,
    val version: Long,
    val updatedAt: Instant,
)

private fun FleetBoard.toResponse() = FleetBoardResponse(
    nodeId = nodeId, nodeName = nodeName, nodeState = nodeState.name.lowercase(),
    id = id, name = name, hash = hash, hubSlide = hubSlide, apiBase = apiBase, guestName = guestName,
    state = state.name.lowercase(),
    maxLanes = maxLanes, assigned = assigned, users = users, version = version, updatedAt = updatedAt,
)
