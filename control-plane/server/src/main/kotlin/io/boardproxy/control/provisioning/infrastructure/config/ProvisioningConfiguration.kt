package io.boardproxy.control.provisioning.infrastructure.config

import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.provisioning.application.AppliedConfigService
import io.boardproxy.control.provisioning.application.BoardRepository
import io.boardproxy.control.provisioning.application.CoreConfigCompiler
import io.boardproxy.control.provisioning.application.DesiredConfigPublisher
import io.boardproxy.control.provisioning.application.DesiredConfigRepository
import io.boardproxy.control.provisioning.application.GrantRepository
import io.boardproxy.control.provisioning.application.KeylinkService
import io.boardproxy.control.provisioning.application.BoardService
import io.boardproxy.control.provisioning.application.NodeService
import io.boardproxy.control.provisioning.application.UserService
import io.boardproxy.control.provisioning.application.NodeRepository
import io.boardproxy.control.provisioning.application.NodeSnapshotRepository
import io.boardproxy.control.provisioning.application.NodeStateLoader
import io.boardproxy.control.provisioning.application.NodeStateService
import io.boardproxy.control.provisioning.application.UserRepository
import io.boardproxy.control.provisioning.infrastructure.compiler.toml.TomlCoreConfigCompiler
import io.boardproxy.control.provisioning.application.NodeHistoryService
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.shared.contracts.ControlTelemetry
import io.boardproxy.control.shared.contracts.QuotaExceededQueries
import io.boardproxy.control.shared.events.OutboxRepository
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import java.time.Clock

@Configuration
class ProvisioningConfiguration {
    @Bean
    fun coreConfigCompiler(): CoreConfigCompiler = TomlCoreConfigCompiler()

    @Bean
    fun controlClock(): Clock = Clock.systemUTC()

    @Bean
    fun appliedConfigService(configs: DesiredConfigRepository) = AppliedConfigService(configs)

    @Bean
    fun nodeHistoryService(
        snapshots: NodeSnapshotRepository,
        nodes: NodeRepository,
        boards: BoardRepository,
        users: UserRepository,
        grants: GrantRepository,
        publisher: DesiredConfigPublisher,
        transactions: TransactionRunner,
        clock: Clock,
    ) = NodeHistoryService(snapshots, nodes, boards, users, grants, publisher, transactions, clock)

    @Bean
    fun keylinkService(
        nodes: NodeRepository,
        boards: BoardRepository,
        users: UserRepository,
        grants: GrantRepository,
        quotas: QuotaExceededQueries,
    ) = KeylinkService(nodes, boards, users, grants, quotas)

    @Bean
    fun nodeStateService(
        nodes: NodeRepository,
        boards: BoardRepository,
        users: UserRepository,
        grants: GrantRepository,
    ) = NodeStateService(nodes, boards, users, grants)

    /**
     * Состояние квот приходит сюда как обычный вход: телеметрия реализует порт
     * из shared, а provisioning о самой телеметрии ничего не знает.
     */
    @Bean
    fun desiredConfigPublisher(
        states: NodeStateLoader,
        quotas: QuotaExceededQueries,
        compiler: CoreConfigCompiler,
        configs: DesiredConfigRepository,
        snapshots: NodeSnapshotRepository,
        audit: AuditRepository,
        outbox: OutboxRepository,
        clock: Clock,
        telemetry: ControlTelemetry,
    ) = DesiredConfigPublisher(states, quotas, compiler, configs, snapshots, audit, outbox, clock, telemetry)

    @Bean
    fun nodeService(
        nodes: NodeRepository,
        publisher: DesiredConfigPublisher,
        transactions: TransactionRunner,
        clock: Clock,
        audit: AuditRepository,
    ) = NodeService(nodes, publisher, transactions, clock, audit)

    @Bean
    fun boardService(
        boards: BoardRepository,
        nodes: NodeRepository,
        publisher: DesiredConfigPublisher,
        transactions: TransactionRunner,
        clock: Clock,
        audit: AuditRepository,
    ) = BoardService(boards, nodes, publisher, transactions, clock, audit)

    @Bean
    fun userService(
        users: UserRepository,
        boards: BoardRepository,
        grants: GrantRepository,
        publisher: DesiredConfigPublisher,
        transactions: TransactionRunner,
        clock: Clock,
        audit: AuditRepository,
    ) = UserService(users, boards, grants, publisher, transactions, clock, audit)
}
