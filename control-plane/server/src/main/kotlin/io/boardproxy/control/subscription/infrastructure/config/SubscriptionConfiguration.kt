package io.boardproxy.control.subscription.infrastructure.config

import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.subscription.application.SubscriptionLinkBuilder
import io.boardproxy.control.access.application.ApiTokenCommands
import io.boardproxy.control.subscription.application.SubscriptionRepository
import io.boardproxy.control.subscription.application.SubscriptionServiceManager
import io.boardproxy.control.subscription.application.SubscriptionServiceRepository
import io.boardproxy.control.subscription.application.SubscriptionService
import io.boardproxy.control.telemetry.application.TrafficQueries
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import com.fasterxml.jackson.databind.ObjectMapper
import java.time.Clock

@Configuration
class SubscriptionConfiguration {

    @Bean
    fun subscriptionLinkBuilder(settings: SubscriptionServiceRepository, json: ObjectMapper) =
        StoredSubscriptionLinkBuilder(settings, json)

    @Bean
    fun subscriptionServiceManager(
        settings: SubscriptionServiceRepository,
        tokens: ApiTokenCommands,
        audit: AuditRepository,
        transactions: TransactionRunner,
        clock: Clock,
    ) = SubscriptionServiceManager(settings, tokens, audit, transactions, clock)

    @Bean
    fun subscriptionService(
        subscriptions: SubscriptionRepository,
        catalogs: CatalogQueries,
        traffic: TrafficQueries,
        audit: AuditRepository,
        transactions: TransactionRunner,
        links: SubscriptionLinkBuilder,
        clock: Clock,
    ) = SubscriptionService(subscriptions, catalogs, traffic, audit, transactions, links, clock)
}
