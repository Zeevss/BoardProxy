package io.boardproxy.control.delivery.infrastructure.persistence.postgres

import org.flywaydb.core.Flyway
import org.springframework.jdbc.core.JdbcTemplate
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.jdbc.datasource.DriverManagerDataSource
import org.testcontainers.postgresql.PostgreSQLContainer
import org.testcontainers.junit.jupiter.Container
import org.testcontainers.junit.jupiter.Testcontainers
import java.time.Duration
import java.time.Instant
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull

@Testcontainers(disabledWithoutDocker = true)
class PostgresNodeSessionLeaseRepositoryTest {
    private val jdbc: JdbcTemplate by lazy {
        JdbcTemplate(DriverManagerDataSource(container.getJdbcUrl(), container.getUsername(), container.getPassword()))
    }
    private val repository by lazy { PostgresNodeSessionLeaseRepository(NamedParameterJdbcTemplate(jdbc)) }

    @BeforeTest
    fun prepare() {
        Flyway.configure().dataSource(container.getJdbcUrl(), container.getUsername(), container.getPassword()).load().migrate()
        jdbc.update("DELETE FROM nodes")
        jdbc.update(
            """INSERT INTO nodes(id,name,state,core_settings,server_key_ciphertext,server_key_nonce,server_key_key_id,resource_version,catalog_version,updated_at,catalog_updated_at)
               VALUES ('node-1','Node 1','enabled','{}',decode('00','hex'),decode('00','hex'),'test',1,1,now(),now())""",
        )
    }

    @Test
    fun `expired owner is fenced and stale owner cannot renew or release successor`() {
        val at = Instant.parse("2026-08-12T12:00:00Z")
        val first = assertNotNull(repository.acquire("node-1", "replica-a", "session-a", at, Duration.ofSeconds(30)))
        assertNull(repository.acquire("node-1", "replica-b", "session-b", at.plusSeconds(1), Duration.ofSeconds(30)))

        val second = assertNotNull(repository.acquire("node-1", "replica-b", "session-b", at.plusSeconds(31), Duration.ofSeconds(30)))
        assertEquals(2, second.fencingToken)
        assertNull(repository.renew(first, at.plusSeconds(32), Duration.ofSeconds(30)))
        repository.release(first)
        assertNotNull(repository.renew(second, at.plusSeconds(32), Duration.ofSeconds(30)))
    }

    companion object {
        @Container
        @JvmField
        val container: PostgreSQLContainer = PostgreSQLContainer("postgres:18-alpine")
    }
}
