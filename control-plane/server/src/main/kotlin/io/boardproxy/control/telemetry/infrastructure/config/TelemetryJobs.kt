package io.boardproxy.control.telemetry.infrastructure.config

import io.boardproxy.control.telemetry.application.TrafficMaintenance
import io.boardproxy.control.telemetry.application.TrafficQuotaService
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.annotation.Value
import org.springframework.scheduling.annotation.Scheduled
import org.springframework.stereotype.Component
import java.time.Clock
import java.time.Duration
import java.time.temporal.ChronoUnit

@Component
class TelemetryJobs(
    private val maintenance: TrafficMaintenance,
    private val quotas: TrafficQuotaService,
    private val clock: Clock,
    @Value("\${control.telemetry.raw-retention-days:31}") private val rawRetentionDays: Long,
    @Value("\${control.telemetry.rollup-retention-days:730}") private val rollupRetentionDays: Long,
) {
    @Scheduled(fixedDelayString = "\${control.telemetry.rollup-delay:PT5M}")
    fun rollup() {
        val end = clock.instant().truncatedTo(ChronoUnit.HOURS)
        runCatching { maintenance.rebuildHourly(end.minus(Duration.ofHours(48)), end) }
            .onFailure { logger.warn("traffic rollup failed", it) }
    }

    @Scheduled(fixedDelayString = "\${control.telemetry.quota-delay:PT1M}")
    fun quotas() {
        runCatching { quotas.evaluate() }.onFailure { logger.warn("traffic quota evaluation failed", it) }
    }

    @Scheduled(cron = "\${control.telemetry.retention-cron:0 17 3 * * *}")
    fun retention() {
        val now = clock.instant()
        runCatching {
            maintenance.deleteRawBefore(now.minus(Duration.ofDays(rawRetentionDays)))
            maintenance.deleteRollupsBefore(now.minus(Duration.ofDays(rollupRetentionDays)))
        }.onFailure { logger.warn("traffic retention failed", it) }
    }

    private companion object {
        val logger = LoggerFactory.getLogger(TelemetryJobs::class.java)
    }
}
