package io.boardproxy.control.telemetry.api.rest

import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.telemetry.application.TrafficKind
import io.boardproxy.control.telemetry.application.TrafficPoint
import io.boardproxy.control.telemetry.application.TrafficQueries
import io.boardproxy.control.telemetry.application.TrafficTotal
import org.springframework.format.annotation.DateTimeFormat
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController
import java.time.Duration
import java.time.Instant

/**
 * Трафик в масштабе флота.
 *
 * Дополняет `/nodes/{nodeId}/traffic`, а не заменяет: тот отвечает на вопрос
 * про конкретную ноду, этот — про весь флот сразу, без `nodeId` в пути.
 *
 * Интерфейсный и пользовательский трафик по-прежнему не смешиваются: `kind`
 * обязателен, и суммы из разных `kind` складывать нельзя — первое считает байты
 * на проводе, второе расшифрованную полезную нагрузку.
 */
@RestController
@RequestMapping("/api/v1/traffic")
@PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
class FleetTrafficController(private val queries: TrafficQueries) {

    /** Разбивка по подписчикам — интерфейсам или пользователям — за период. */
    @GetMapping("/totals")
    fun totals(
        @RequestParam(required = false) nodeId: String?,
        @RequestParam kind: String,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) from: Instant,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) to: Instant,
    ): List<TrafficTotal> {
        validateRange(from, to)
        val scope = nodeId?.takeIf(String::isNotBlank)
        return when (kind(kind)) {
            TrafficKind.INTERFACE -> queries.interfaceTotals(scope, from, to)
            TrafficKind.USER -> queries.userTotals(scope, from, to)
        }
    }

    /** Разбивка по нодам. Всегда по всему флоту: сузить её до одной ноды незачем. */
    @GetMapping("/by-node")
    fun byNode(
        @RequestParam kind: String,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) from: Instant,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) to: Instant,
    ): List<TrafficTotal> {
        validateRange(from, to)
        return queries.nodeTotals(kind(kind), from, to)
    }

    @GetMapping("/series")
    fun series(
        @RequestParam(required = false) nodeId: String?,
        @RequestParam kind: String,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) from: Instant,
        @RequestParam @DateTimeFormat(iso = DateTimeFormat.ISO.DATE_TIME) to: Instant,
        @RequestParam(defaultValue = "3600") bucketSeconds: Long,
    ): List<TrafficPoint> {
        validateRange(from, to)
        if (bucketSeconds !in MINIMUM_BUCKET..MAXIMUM_BUCKET) {
            throw InvalidRequest("bucketSeconds must be between $MINIMUM_BUCKET and $MAXIMUM_BUCKET")
        }
        return queries.series(nodeId?.takeIf(String::isNotBlank), kind(kind), from, to, bucketSeconds)
    }

    private fun kind(value: String): TrafficKind =
        runCatching { TrafficKind.valueOf(value.trim().uppercase()) }
            .getOrElse { throw InvalidRequest("traffic kind must be interface or user") }

    private fun validateRange(from: Instant, to: Instant) {
        if (!from.isBefore(to) || Duration.between(from, to) > MAX_RANGE) {
            throw InvalidRequest("traffic range must be positive and at most 31 days")
        }
    }

    private companion object {
        val MAX_RANGE: Duration = Duration.ofDays(31)
        const val MINIMUM_BUCKET = 10L
        const val MAXIMUM_BUCKET = 86_400L
    }
}
