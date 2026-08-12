package io.boardproxy.control.access.infrastructure.config

import io.boardproxy.control.access.application.ApiTokenRepository
import io.boardproxy.control.access.application.ApiTokenService
import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.shared.persistence.TransactionRunner
import org.springframework.beans.factory.annotation.Value
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import java.time.Clock

@Configuration
class AccessConfiguration {
    @Bean
    fun apiTokenService(
        tokens: ApiTokenRepository,
        audit: AuditRepository,
        transactions: TransactionRunner,
        clock: Clock,
        @Value("\${control.access.bootstrap-admin-token:}") bootstrapToken: String,
    ) = ApiTokenService(tokens, audit, transactions, clock, bootstrapToken)
}
