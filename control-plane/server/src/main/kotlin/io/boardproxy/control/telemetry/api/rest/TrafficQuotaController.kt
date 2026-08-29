package io.boardproxy.control.telemetry.api.rest

import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceNotFound
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
import java.security.Principal

/**
 * Квота принадлежит пользователю, а не паре «нода + тег», поэтому и адресуется
 * через пользователя. Расход в ответе — сумма по всем нодам его размещения.
 */
@RestController
@RequestMapping("/api/v1/users/{userId}/quota")
class TrafficQuotaController(private val service: TrafficQuotaService) {

    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun get(@PathVariable userId: String): TrafficQuotaUsage =
        service.get(userId) ?: throw ResourceNotFound("traffic quota for user $userId not found")

    @PutMapping
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun put(
        @PathVariable userId: String,
        @RequestHeader("If-Match", required = false) ifMatch: String?,
        @RequestBody request: TrafficQuotaRequest,
        principal: Principal,
    ): ResponseEntity<TrafficQuota> {
        val expected = ifMatch?.removeSurrounding("\"")?.toLongOrNull()
        if (ifMatch != null && expected == null) throw InvalidRequest("If-Match must contain the numeric quota version")
        val quota = service.put(
            userId,
            enum(request.period, "period", QuotaPeriod::valueOf),
            request.limitBytes,
            enum(request.action, "action", QuotaAction::valueOf),
            request.enabled,
            expected,
            principal.name,
        )
        return ResponseEntity.ok().eTag(quota.version.toString()).body(quota)
    }

    @DeleteMapping
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun delete(
        @PathVariable userId: String,
        @RequestHeader("If-Match") ifMatch: String,
        principal: Principal,
    ): ResponseEntity<Void> {
        val expected = ifMatch.removeSurrounding("\"").toLongOrNull()
            ?: throw InvalidRequest("If-Match must contain the numeric quota version")
        service.delete(userId, expected, principal.name)
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
