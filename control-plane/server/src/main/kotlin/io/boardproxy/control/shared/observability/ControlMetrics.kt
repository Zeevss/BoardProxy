package io.boardproxy.control.shared.observability

import io.boardproxy.control.shared.agents.AgentRegistry
import io.boardproxy.control.shared.agents.AgentStatusRepository
import io.boardproxy.control.shared.contracts.ControlTelemetry
import io.micrometer.core.instrument.MeterRegistry
import org.springframework.stereotype.Component
import java.time.Clock

/**
 * Метрики, отвечающие на вопросы, которые действительно задают в проде.
 *
 * `published` и `unchanged` считаются отдельно намеренно: перекос в сторону
 * published означает, что конфигурация пересобирается, не меняя байт, то есть
 * ядро перезапускается впустую. Именно этот класс ошибок новая публикация и
 * устраняет — пусть он будет виден на графике, а не в логах.
 */
@Component
class ControlMetrics(
    registry: MeterRegistry,
    private val agents: AgentRegistry,
    private val statuses: AgentStatusRepository,
    private val clock: Clock,
) : ControlTelemetry {

    private val published = registry.counter("control.config.published")
    private val unchanged = registry.counter("control.config.unchanged")
    private val accepted = registry.counter("control.reports.accepted")
    private val duplicated = registry.counter("control.reports.duplicated")

    init {
        registry.gauge("control.agents.total", this) { it.agents.list().size.toDouble() }
        registry.gauge("control.agents.offline", this) { it.offlineAgents().toDouble() }
        registry.gauge("control.agents.apply_failed", this) { metrics ->
            metrics.statuses.list().count { !it.applyError.isNullOrBlank() }.toDouble()
        }
    }

    override fun configPublished(changed: Boolean) {
        if (changed) published.increment() else unchanged.increment()
    }

    override fun reportAccepted(fresh: Boolean) {
        if (fresh) accepted.increment() else duplicated.increment()
    }

    /** Онлайн считается при чтении: хранимого флага, который мог бы устареть, нет. */
    private fun offlineAgents(): Int {
        val now = clock.instant()
        val online = statuses.list().count { it.online(now) }
        return (agents.list().size - online).coerceAtLeast(0)
    }
}
