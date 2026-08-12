package io.boardproxy.control.telemetry.api.rest

import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.telemetry.application.TrafficQuotaService
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import io.boardproxy.control.telemetry.domain.TrafficQuota
import io.boardproxy.control.telemetry.domain.TrafficQuotaUsage
import org.springframework.http.ResponseEntity
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.DeleteMapping
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PutMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestHeader
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController

@RestController
@RequestMapping("/api/v1/nodes/{nodeId}/traffic/quotas")
class TrafficQuotaController(private val service: TrafficQuotaService) {
    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun list(@PathVariable nodeId: String): List<TrafficQuotaUsage> = service.list(nodeId)

    @PutMapping("/{userTag}")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun put(
        @PathVariable nodeId: String,
        @PathVariable userTag: String,
        @RequestHeader("If-Match", required = false) ifMatch: String?,
        @RequestBody request: TrafficQuotaRequest,
    ): ResponseEntity<TrafficQuota> {
        val expected = ifMatch?.removeSurrounding("\"")?.toLongOrNull()
        if (ifMatch != null && expected == null) throw InvalidRequest("If-Match must contain the numeric quota version")
        val quota = service.put(
            nodeId, userTag,
            enum(request.period, "period", QuotaPeriod::valueOf),
            request.limitBytes,
            enum(request.action, "action", QuotaAction::valueOf),
            request.enabled,
            expected,
        )
        return ResponseEntity.ok().eTag(quota.version.toString()).body(quota)
    }

    @DeleteMapping("/{userTag}")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun delete(
        @PathVariable nodeId: String,
        @PathVariable userTag: String,
        @RequestHeader("If-Match") ifMatch: String,
    ): ResponseEntity<Void> {
        val expected = ifMatch.removeSurrounding("\"").toLongOrNull()
            ?: throw InvalidRequest("If-Match must contain the numeric quota version")
        service.delete(nodeId, userTag, expected)
        return ResponseEntity.noContent().build()
    }

    private fun <T> enum(value: String, field: String, parser: (String) -> T): T =
        runCatching { parser(value.trim().uppercase()) }
            .getOrElse { throw InvalidRequest("invalid traffic quota $field") }
}

data class TrafficQuotaRequest(
    val period: String = "monthly",
    val limitBytes: Long,
    val action: String = "alert",
    val enabled: Boolean = true,
)
