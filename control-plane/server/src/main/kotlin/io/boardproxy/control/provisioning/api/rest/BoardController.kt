package io.boardproxy.control.provisioning.api.rest

import io.boardproxy.control.provisioning.application.BoardInput
import io.boardproxy.control.provisioning.application.BoardService
import io.boardproxy.control.provisioning.application.Page
import io.boardproxy.control.provisioning.domain.model.Board
import org.springframework.http.HttpStatus
import org.springframework.http.ResponseEntity
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.DeleteMapping
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.PutMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestHeader
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController
import java.security.Principal
import java.time.Instant

/**
 * Борд адресуется парой «нода + борд»: его идентичность включает ноду, потому
 * что hub-slide не делится между серверами.
 */
@RestController
@RequestMapping("/api/v1/boards")
class BoardController(private val service: BoardService) {

    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun list(
        @RequestParam(required = false) query: String?,
        @RequestParam(required = false) nodeId: String?,
        @RequestParam(defaultValue = "0") offset: Int,
        @RequestParam(defaultValue = "50") limit: Int,
    ): Page<BoardResponse> = service.list(query, nodeId, offset, limit.coerceIn(1, MAXIMUM_PAGE))
        .let { Page(it.items.map(Board::toResponse), it.offset, it.limit, it.total) }

    @GetMapping("/{nodeId}/{id}")
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun get(@PathVariable nodeId: String, @PathVariable id: String): ResponseEntity<BoardResponse> =
        service.get(nodeId, id).ok()

    @PostMapping
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun create(@RequestBody request: BoardRequest, principal: Principal): ResponseEntity<BoardResponse> {
        val board = service.create(request.toInput(), principal.name)
        return ResponseEntity.status(HttpStatus.CREATED).eTag(board.version.toString()).body(board.toResponse())
    }

    @PutMapping("/{nodeId}/{id}")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun update(
        @PathVariable nodeId: String,
        @PathVariable id: String,
        @RequestHeader("If-Match") ifMatch: String,
        @RequestBody request: BoardRequest,
        principal: Principal,
    ): ResponseEntity<BoardResponse> =
        service.update(nodeId, id, ifMatch.version("board"), request.toInput(), principal.name).ok()

    @DeleteMapping("/{nodeId}/{id}")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun delete(
        @PathVariable nodeId: String,
        @PathVariable id: String,
        @RequestHeader("If-Match") ifMatch: String,
        principal: Principal,
    ): ResponseEntity<Void> {
        service.delete(nodeId, id, ifMatch.version("board"), principal.name)
        return ResponseEntity.noContent().build()
    }

    private fun Board.ok() = ResponseEntity.ok().eTag(version.toString()).body(toResponse())

    private companion object {
        const val MAXIMUM_PAGE = 200
    }
}

data class BoardRequest(
    val id: String? = null,
    val nodeId: String,
    val name: String,
    val hash: String,
    val hubSlide: String? = null,
    val apiBase: String? = null,
    val guestName: String? = null,
    val state: String = "enabled",
    val maxLanes: Int = 4,
)

data class BoardResponse(
    val nodeId: String,
    val id: String,
    val name: String,
    val hash: String,
    val hubSlide: String?,
    val apiBase: String?,
    val guestName: String?,
    val state: String,
    val maxLanes: Int,
    val version: Long,
    val updatedAt: Instant,
)

private fun BoardRequest.toInput() = BoardInput(
    id = id, nodeId = nodeId, name = name, hash = hash, hubSlide = hubSlide,
    apiBase = apiBase, guestName = guestName, state = state.resourceState(), maxLanes = maxLanes,
)

private fun Board.toResponse() = BoardResponse(
    nodeId = nodeId, id = id, name = name, hash = hash, hubSlide = hubSlide, apiBase = apiBase,
    guestName = guestName, state = state.name.lowercase(), maxLanes = maxLanes,
    version = version, updatedAt = updatedAt,
)
