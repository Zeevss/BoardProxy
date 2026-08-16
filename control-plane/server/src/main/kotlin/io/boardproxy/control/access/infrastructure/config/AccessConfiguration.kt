package io.boardproxy.control.access.infrastructure.config

import io.boardproxy.control.access.application.ApiTokenRepository
import io.boardproxy.control.access.application.ApiTokenService
import io.boardproxy.control.access.application.AccessAuthenticator
import io.boardproxy.control.access.application.PanelAccessRepository
import io.boardproxy.control.access.application.PanelAuthService
import io.boardproxy.control.access.application.PasswordHasher
import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.shared.persistence.TransactionRunner
import org.springframework.context.annotation.Primary
import org.springframework.beans.factory.annotation.Value
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import org.springframework.security.crypto.bcrypt.BCryptPasswordEncoder
import java.time.Clock
import java.time.Duration

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

    @Bean
    fun passwordHasher(): PasswordHasher {
        val encoder = BCryptPasswordEncoder(12)
        return object : PasswordHasher {
            override fun hash(password: String): String = requireNotNull(encoder.encode(password))
            override fun matches(password: String, encoded: String): Boolean = encoder.matches(password, encoded)
        }
    }

    @Bean
    fun panelAuthService(
        repository: PanelAccessRepository,
        passwords: PasswordHasher,
        audit: AuditRepository,
        transactions: TransactionRunner,
        clock: Clock,
        @Value("\${control.access.panel-session-ttl:PT12H}") sessionTtl: Duration,
    ) = PanelAuthService(repository, passwords, audit, transactions, clock, sessionTtl)

    @Bean
    @Primary
    fun accessAuthenticator(
        panel: PanelAuthService,
        apiTokens: ApiTokenService,
    ): AccessAuthenticator = AccessAuthenticator { rawToken ->
        panel.authenticate(rawToken) ?: apiTokens.authenticate(rawToken)
    }
}
