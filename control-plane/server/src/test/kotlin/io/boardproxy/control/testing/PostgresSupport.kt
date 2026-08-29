package io.boardproxy.control.testing

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule
import com.fasterxml.jackson.module.kotlin.KotlinModule
import io.boardproxy.control.shared.security.AesGcmSecretCipher
import io.boardproxy.control.shared.security.SecretCipher
import org.flywaydb.core.Flyway
import org.springframework.jdbc.core.JdbcTemplate
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.jdbc.datasource.DriverManagerDataSource
import org.testcontainers.DockerClientFactory
import org.testcontainers.postgresql.PostgreSQLContainer
import javax.sql.DataSource

/**
 * Один Postgres на весь прогон тестов. Поднимать контейнер на каждый класс
 * дороже самих тестов, а схема между классами не меняется — меняются только
 * данные, которые чистит [truncate].
 *
 * Останавливать контейнер не нужно: за этим следит Ryuk.
 */
object PostgresSupport {
    val dockerAvailable: Boolean by lazy {
        runCatching { DockerClientFactory.instance().isDockerAvailable }.getOrDefault(false)
    }

    private val container: PostgreSQLContainer by lazy {
        PostgreSQLContainer("postgres:18-alpine").apply { start() }
    }

    val dataSource: DataSource by lazy {
        DriverManagerDataSource(container.jdbcUrl, container.username, container.password).also { source ->
            Flyway.configure().dataSource(source).load().migrate()
        }
    }

    val jdbc: JdbcTemplate by lazy { JdbcTemplate(dataSource) }

    val named: NamedParameterJdbcTemplate by lazy { NamedParameterJdbcTemplate(jdbc) }

    val json: ObjectMapper by lazy {
        ObjectMapper().registerModule(KotlinModule.Builder().build()).registerModule(JavaTimeModule())
    }

    val cipher: SecretCipher by lazy {
        AesGcmSecretCipher(java.util.Base64.getEncoder().encodeToString(ByteArray(32) { 7 }), "test-key-v1")
    }

    /**
     * Чистит данные, не трогая схему. Достаточно перечислить корни: остальное
     * уносит ON DELETE CASCADE, и это заодно проверяет, что каскады настроены.
     */
    fun truncate() {
        jdbc.update(
            """TRUNCATE agents, users, credentials, panel_administrators,
                        traffic_hourly_rollups, user_traffic_lifetime_totals,
                        quota_config_changes, audit_events, outbox_events
               RESTART IDENTITY CASCADE""",
        )
    }
}
