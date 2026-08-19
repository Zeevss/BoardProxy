package io.boardproxy.control.provisioning.api.rest

import io.boardproxy.control.provisioning.application.AppliedConfig
import io.boardproxy.control.provisioning.application.AppliedConfigQueries
import io.boardproxy.control.shared.errors.ResourceNotFound
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import java.time.Instant

/** Применённая конфигурация ноды без клиентских идентичностей и приватных ключей. */
@RestController
@RequestMapping("/api/v1/nodes/{nodeId}/config")
class AppliedConfigController(private val queries: AppliedConfigQueries) {
    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun latest(@PathVariable nodeId: String): AppliedConfigResponse =
        queries.latest(nodeId)?.toResponse() ?: throw ResourceNotFound("node $nodeId has no applied configuration")
}

data class AppliedConfigResponse(
    val nodeId: String,
    val revision: Long,
    val configSha256: String,
    val toml: String,
    val updatedAt: Instant,
)

private fun AppliedConfig.toResponse() =
    AppliedConfigResponse(nodeId, revision, configSha256, toml, updatedAt)
