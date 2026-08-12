package io.boardproxy.control.telemetry.api.rest

import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.telemetry.application.TrafficQueries
import io.boardproxy.control.telemetry.application.TrafficTotal
import io.boardproxy.control.telemetry.application.TrafficKind
import io.boardproxy.control.telemetry.application.TrafficPoint
import org.springframework.format.annotation.DateTimeFormat
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController
import java.time.Duration
import java.time.Instant

@RestController
@RequestMapping("/api/v1/nodes/{nodeId}/traffic")
@PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
class TrafficController(private val queries: TrafficQueries) {
    @GetMapping("/interfaces")
    fun interfaces(
        @PathVariable nodeId: String,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) from: Instant,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) to: Instant,
    ): List<TrafficTotal> {
        validateRange(from, to)
        return queries.interfaceTotals(nodeId, from, to)
    }

    @GetMapping("/users")
    fun users(
        @PathVariable nodeId: String,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) from: Instant,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) to: Instant,
    ): List<TrafficTotal> {
        validateRange(from, to)
        return queries.userTotals(nodeId, from, to)
    }

    @GetMapping("/series")
    fun series(
        @PathVariable nodeId: String,
        @RequestParam kind: String,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) from: Instant,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) to: Instant,
        @RequestParam(defaultValue = "300") bucketSeconds: Long,
    ): List<TrafficPoint> {
        validateRange(from, to)
        if (bucketSeconds !in 10..86_400) throw InvalidRequest("bucketSeconds must be between 10 and 86400")
        val type = runCatching { TrafficKind.valueOf(kind.trim().uppercase()) }
            .getOrElse { throw InvalidRequest("traffic kind must be interface or user") }
        return queries.series(nodeId, type, from, to, bucketSeconds)
    }

    private fun validateRange(from: Instant, to: Instant) {
        if (!from.isBefore(to) || Duration.between(from, to) > MAX_RANGE) {
            throw InvalidRequest("traffic range must be positive and at most 31 days")
        }
    }

    private companion object {
        val MAX_RANGE: Duration = Duration.ofDays(31)
    }
}
