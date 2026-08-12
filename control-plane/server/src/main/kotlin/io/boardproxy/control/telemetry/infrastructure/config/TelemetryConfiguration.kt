package io.boardproxy.control.telemetry.infrastructure.config

import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.provisioning.application.CatalogResourceCommands
import io.boardproxy.control.telemetry.application.TrafficQuotaNotifier
import io.boardproxy.control.telemetry.application.TrafficQuotaRepository
import io.boardproxy.control.telemetry.application.TrafficQuotaService
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import java.time.Clock

@Configuration
class TelemetryConfiguration {
    @Bean
    fun trafficQuotaService(
        quotas: TrafficQuotaRepository,
        catalogs: CatalogQueries,
        resources: CatalogResourceCommands,
        notifier: TrafficQuotaNotifier,
        clock: Clock,
    ) = TrafficQuotaService(quotas, catalogs, resources, notifier, clock)
}
