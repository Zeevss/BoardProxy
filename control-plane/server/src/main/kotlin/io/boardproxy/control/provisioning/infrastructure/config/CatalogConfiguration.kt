package io.boardproxy.control.provisioning.infrastructure.config

import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.provisioning.application.CatalogRepository
import io.boardproxy.control.provisioning.application.CatalogService
import io.boardproxy.control.provisioning.application.ConfigRevisionRepository
import io.boardproxy.control.provisioning.application.CoreConfigCompiler
import io.boardproxy.control.provisioning.infrastructure.compiler.toml.TomlCoreConfigCompiler
import io.boardproxy.control.shared.events.OutboxRepository
import io.boardproxy.control.shared.persistence.TransactionRunner
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import java.time.Clock

@Configuration
class CatalogConfiguration {
    @Bean
    fun coreConfigCompiler(): CoreConfigCompiler = TomlCoreConfigCompiler()

    @Bean
    fun controlClock(): Clock = Clock.systemUTC()

    @Bean
    fun catalogService(
        catalogs: CatalogRepository,
        snapshots: io.boardproxy.control.provisioning.application.CatalogSnapshotRepository,
        compiler: CoreConfigCompiler,
        revisions: ConfigRevisionRepository,
        audit: AuditRepository,
        outbox: OutboxRepository,
        transactions: TransactionRunner,
        clock: Clock,
    ) = CatalogService(catalogs, snapshots, compiler, revisions, audit, outbox, transactions, clock)

    @Bean
    fun catalogResourceService(
        catalogs: CatalogService,
        clock: Clock,
    ) = io.boardproxy.control.provisioning.application.CatalogResourceService(catalogs, catalogs, clock)

    @Bean
    fun catalogHistoryService(
        catalogs: CatalogService,
        snapshots: io.boardproxy.control.provisioning.application.CatalogSnapshotRepository,
        clock: Clock,
    ) = io.boardproxy.control.provisioning.application.CatalogHistoryService(catalogs, snapshots, catalogs, clock)

    @Bean
    fun catalogOverviewQueries(catalogs: CatalogService) =
        io.boardproxy.control.provisioning.application.CatalogOverviewQueries(catalogs::search)
}
