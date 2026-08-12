package io.boardproxy.control.fleet.domain

import com.fasterxml.jackson.annotation.JsonProperty
import java.time.Instant

data class EnrollmentToken(val plaintext: String, val expiresAt: Instant)

data class IssuedCertificate(
    val serialNumber: String,
    val certificatePem: ByteArray,
    val caCertificatePem: ByteArray,
    val issuedAt: Instant,
    val expiresAt: Instant,
)

data class NodeCertificate(
    val serialNumber: String,
    val nodeId: String,
    val fingerprintSha256: String,
    val issuedAt: Instant,
    val expiresAt: Instant,
    val revokedAt: Instant?,
    val revokedReason: String?,
    val lastSeenAt: Instant?,
)

data class BootstrapSecret(
    @JsonProperty("node_id")
    val nodeId: String,
    @JsonProperty("hub_url")
    val hubUrl: String,
    @JsonProperty("enrollment_token")
    val enrollmentToken: String,
    @JsonProperty("ca_certificate_pem")
    val caCertificatePem: String,
)
