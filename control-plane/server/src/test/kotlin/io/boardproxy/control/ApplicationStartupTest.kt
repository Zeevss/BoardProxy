package io.boardproxy.control

import org.springframework.boot.test.context.SpringBootTest
import org.springframework.test.context.DynamicPropertyRegistry
import org.springframework.test.context.DynamicPropertySource
import org.testcontainers.junit.jupiter.Container
import org.testcontainers.junit.jupiter.Testcontainers
import org.testcontainers.postgresql.PostgreSQLContainer
import java.nio.file.Files
import kotlin.test.Test

@SpringBootTest(
    webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT,
    properties = [
        "control.grpc.port=0",
        "control.events.outbox-delay=PT1H",
        "control.events.sse-heartbeat-delay=PT1H",
        "control.delivery.status-expiry-delay=PT1H",
        "control.telemetry.rollup-delay=PT1H",
        "control.telemetry.quota-delay=PT1H",
    ],
)
@Testcontainers(disabledWithoutDocker = true)
class ApplicationStartupTest {
    @Test
    fun `full application context starts with production security and migrations`() = Unit

    companion object {
        private val pkiDirectory = Files.createTempDirectory("boardproxy-startup-pki")

        @Container
        @JvmField
        val postgres: PostgreSQLContainer = PostgreSQLContainer("postgres:18-alpine")

        @JvmStatic
        @DynamicPropertySource
        fun properties(registry: DynamicPropertyRegistry) {
            registry.add("spring.datasource.url", postgres::getJdbcUrl)
            registry.add("spring.datasource.username", postgres::getUsername)
            registry.add("spring.datasource.password", postgres::getPassword)
            registry.add("control.security.master-key") { "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" }
            registry.add("control.security.master-key-id") { "test-v1" }
            registry.add("control.pki.directory") { pkiDirectory.toString() }
            registry.add("control.grpc.server-names") { "localhost" }
        }
    }
}
