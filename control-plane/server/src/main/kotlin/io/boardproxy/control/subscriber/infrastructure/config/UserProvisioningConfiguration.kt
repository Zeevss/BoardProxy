package io.boardproxy.control.subscriber.infrastructure.config

import io.boardproxy.control.provisioning.application.CatalogCommands
import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.subscriber.application.UserProvisioningService
import io.boardproxy.control.subscription.application.SubscriptionCommands
import io.boardproxy.control.subscription.application.SubscriptionLinkBuilder
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import java.time.Clock

@Configuration
class UserProvisioningConfiguration {
    @Bean
    fun userProvisioningService(
        catalogs: CatalogQueries,
        catalogCommands: CatalogCommands,
        subscriptions: SubscriptionCommands,
        links: SubscriptionLinkBuilder,
        transactions: TransactionRunner,
        clock: Clock,
    ) = UserProvisioningService(catalogs, catalogCommands, subscriptions, links, transactions, clock)
}
