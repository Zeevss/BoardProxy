package io.boardproxy.control.delivery.infrastructure.config

import io.boardproxy.control.delivery.application.NodeReportService
import io.boardproxy.control.delivery.application.NodeRuntimeSink
import io.boardproxy.control.delivery.application.NodeTrafficSink
import io.boardproxy.control.shared.agents.AgentCommandRepository
import io.boardproxy.control.shared.contracts.ControlTelemetry
import io.boardproxy.control.shared.agents.AgentReportLog
import io.boardproxy.control.shared.agents.AgentStatusRepository
import io.boardproxy.control.shared.persistence.TransactionRunner
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import java.time.Clock

@Configuration
class DeliveryConfiguration {
    @Bean
    fun nodeReportService(
        reports: AgentReportLog,
        statuses: AgentStatusRepository,
        commands: AgentCommandRepository,
        traffic: NodeTrafficSink,
        runtime: NodeRuntimeSink,
        transactions: TransactionRunner,
        clock: Clock,
        telemetry: ControlTelemetry,
    ) = NodeReportService(reports, statuses, commands, traffic, runtime, transactions, clock, telemetry)
}
