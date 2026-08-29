package io.boardproxy.control.fleet.infrastructure.config

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.fleet.application.CertificateAuthority
import io.boardproxy.control.fleet.application.EnrollmentService
import io.boardproxy.control.fleet.application.EnrollmentTokenRepository
import io.boardproxy.control.fleet.application.ServerCertificateAuthority
import io.boardproxy.control.fleet.application.NodeCertificateRepository
import io.boardproxy.control.fleet.application.NodeConnectionPolicy
import io.boardproxy.control.fleet.application.NodeCertificateService
import io.boardproxy.control.fleet.infrastructure.pki.FileCertificateAuthority
import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.shared.security.SecretCipher
import org.springframework.beans.factory.annotation.Value
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import java.nio.file.Path
import java.time.Clock

@Configuration
class FleetConfiguration {
    @Bean
    fun certificateAuthority(
        secrets: SecretCipher,
        json: ObjectMapper,
        clock: Clock,
        @Value("\${control.pki.directory}") directory: String,
        @Value("\${control.grpc.server-names}") names: String,
    ): ServerCertificateAuthority = FileCertificateAuthority(
        Path.of(directory), names.split(',').map(String::trim).filter(String::isNotEmpty), secrets, json, clock,
    )

    @Bean
    fun enrollmentService(
        tokens: EnrollmentTokenRepository,
        authority: CertificateAuthority,
        certificates: NodeCertificateRepository,
        json: ObjectMapper,
        audit: AuditRepository,
        transactions: TransactionRunner,
        clock: Clock,
    ) = EnrollmentService(tokens, authority, certificates, json, audit, transactions, clock)

    @Bean
    fun nodeCertificateService(
        certificates: NodeCertificateRepository,
        audit: AuditRepository,
        transactions: TransactionRunner,
        clock: Clock,
    ) = NodeCertificateService(certificates, audit, transactions, clock)

    @Bean
    fun nodeConnectionPolicy(
        certificates: NodeCertificateRepository,
        clock: Clock,
    ) = NodeConnectionPolicy { nodeId, certificate ->
        if (!certificates.nodeEnabled(nodeId)) return@NodeConnectionPolicy false
        val serial = certificate.serialNumber.toString(16)
        certificates.active(nodeId, serial, clock.instant()).also { allowed ->
            if (allowed) certificates.touch(nodeId, serial, clock.instant())
        }
    }
}
