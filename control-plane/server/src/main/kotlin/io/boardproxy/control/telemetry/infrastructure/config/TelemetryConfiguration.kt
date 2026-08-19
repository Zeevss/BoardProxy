package io.boardproxy.control.telemetry.infrastructure.config

import io.boardproxy.control.shared.contracts.QuotaExceededQueries
import io.boardproxy.control.shared.events.OutboxRepository
import io.boardproxy.control.telemetry.application.TrafficQuotaNotifier
import io.boardproxy.control.telemetry.application.TrafficQuotaRepository
import io.boardproxy.control.telemetry.application.TrafficQuotaService
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import java.time.Clock

@Configuration
class TelemetryConfiguration {
    /**
     * Сервис сам реализует [QuotaExceededQueries], поэтому компилятор
     * конфигурации получает состояние квот через порт из shared: provisioning
     * не знает о телеметрии, телеметрия не знает о provisioning.
     */
    @Bean
    fun trafficQuotaService(
        quotas: TrafficQuotaRepository,
        notifier: TrafficQuotaNotifier,
        outbox: OutboxRepository,
        clock: Clock,
    ) = TrafficQuotaService(quotas, notifier, outbox, clock)
}
