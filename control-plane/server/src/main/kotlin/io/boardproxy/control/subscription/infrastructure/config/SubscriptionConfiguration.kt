package io.boardproxy.control.subscription.infrastructure.config

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.shared.contracts.ServiceTokenIssuer
import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.contracts.KeylinkQueries
import io.boardproxy.control.shared.contracts.UserUsageQueries
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.subscription.application.SubscriptionLinkBuilder
import io.boardproxy.control.subscription.application.SubscriptionRepository
import io.boardproxy.control.subscription.application.SubscriptionService
import io.boardproxy.control.subscription.application.SubscriptionServiceManager
import io.boardproxy.control.subscription.application.SubscriptionServiceRepository
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import java.time.Clock

@Configuration
class SubscriptionConfiguration {

    @Bean
    fun subscriptionLinkBuilder(settings: SubscriptionServiceRepository, json: ObjectMapper) =
        StoredSubscriptionLinkBuilder(settings, json)

    @Bean
    fun subscriptionServiceManager(
        settings: SubscriptionServiceRepository,
        tokens: ServiceTokenIssuer,
        audit: AuditRepository,
        transactions: TransactionRunner,
        clock: Clock,
    ) = SubscriptionServiceManager(settings, tokens, audit, transactions, clock)

    @Bean
    fun subscriptionService(
        subscriptions: SubscriptionRepository,
        keylinks: KeylinkQueries,
        usage: UserUsageQueries,
        audit: AuditRepository,
        transactions: TransactionRunner,
        links: SubscriptionLinkBuilder,
        clock: Clock,
    ) = SubscriptionService(subscriptions, keylinks, usage, audit, transactions, links, clock)
}
