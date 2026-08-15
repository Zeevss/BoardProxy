package io.boardproxy.control.subscription.infrastructure.config

import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.subscription.application.SubscriptionRepository
import io.boardproxy.control.subscription.application.SubscriptionService
import io.boardproxy.control.telemetry.application.TrafficQueries
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import org.springframework.beans.factory.annotation.Value
import com.fasterxml.jackson.databind.ObjectMapper
import java.time.Clock

@Configuration
class SubscriptionConfiguration {

    @Bean
    fun subscriptionLinkBuilder(
        json: ObjectMapper,
        @Value("\${control.subscription.enabled:false}") enabled: Boolean,
        @Value("\${control.subscription.public-url:}") publicUrl: String,
        @Value("\${control.subscription.yandex-editor-url:}") yandexEditorUrl: String,
        @Value("\${control.subscription.recovery-key-id:}") recoveryKeyId: String,
        @Value("\${control.subscription.recovery-server-public-key:}") recoveryServerPublicKey: String,
    ) = ConfiguredSubscriptionLinkBuilder(
        enabled, publicUrl, yandexEditorUrl, recoveryKeyId, recoveryServerPublicKey, json,
    )

    @Bean
    fun subscriptionService(
        subscriptions: SubscriptionRepository,
        catalogs: CatalogQueries,
        traffic: TrafficQueries,
        audit: AuditRepository,
        transactions: TransactionRunner,
        clock: Clock,
    ) = SubscriptionService(subscriptions, catalogs, traffic, audit, transactions, clock)
}
