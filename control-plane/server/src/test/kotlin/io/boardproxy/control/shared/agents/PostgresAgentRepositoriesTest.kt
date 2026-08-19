package io.boardproxy.control.shared.agents

import io.boardproxy.control.shared.agents.postgres.PostgresAgentCommandRepository
import io.boardproxy.control.shared.agents.postgres.PostgresAgentRegistry
import io.boardproxy.control.shared.agents.postgres.PostgresAgentReportLog
import io.boardproxy.control.shared.agents.postgres.PostgresAgentStatusRepository
import io.boardproxy.control.testing.PostgresSupport
import io.boardproxy.control.testing.TEST_TIME
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class PostgresAgentRepositoriesTest {
    private val agents = PostgresAgentRegistry(PostgresSupport.named)
    private val statuses = PostgresAgentStatusRepository(PostgresSupport.named, PostgresSupport.json)
    private val commands = PostgresAgentCommandRepository(PostgresSupport.named)
    private val reports = PostgresAgentReportLog(PostgresSupport.named)

    @BeforeTest
    fun prepare() {
        assertTrue(PostgresSupport.dockerAvailable, "тесты агентов требуют Docker")
        PostgresSupport.truncate()
        agents.register(Agent("node-1", AgentKind.NODE, "Первая нода"))
        agents.register(Agent("subscription-service", AgentKind.SUBSCRIPTION_SERVICE, "Сервис подписок"))
    }

    @Test
    fun `нода и сервис подписок лежат в одном реестре`() {
        assertEquals(
            listOf(AgentKind.NODE, AgentKind.SUBSCRIPTION_SERVICE),
            agents.list().map(Agent::kind),
        )
        assertEquals("Первая нода", assertNotNull(agents.find("node-1")).name)
    }

    @Test
    fun `состояние перезаписывается целиком и хранит поля вида агента`() {
        statuses.record(
            AgentStatus(
                agentId = "subscription-service", appliedRevision = 7, agentVersion = "1.4.0",
                lastReportAt = TEST_TIME, details = mapOf("recoveryWatcherReady" to true),
            ),
        )

        val stored = assertNotNull(statuses.find("subscription-service"))

        assertEquals(7, stored.appliedRevision)
        assertEquals("1.4.0", stored.agentVersion)
        assertEquals(true, stored.details["recoveryWatcherReady"])
    }

    /** Онлайн вычисляется при чтении, поэтому фоновая job для его устаревания не нужна. */
    @Test
    fun `онлайн определяется свежестью отчёта`() {
        statuses.record(AgentStatus(agentId = "node-1", lastReportAt = TEST_TIME))

        val stored = assertNotNull(statuses.find("node-1"))

        assertTrue(stored.online(TEST_TIME.plusSeconds(30)))
        assertFalse(stored.online(TEST_TIME.plusSeconds(60)))
        assertFalse(AgentStatus(agentId = "node-1").online(TEST_TIME), "не отчитывался ни разу")
    }

    @Test
    fun `команда доставляется ровно один раз`() {
        val nonce = commands.issue("subscription-service", "restart", "operator", TEST_TIME)

        assertEquals(1, nonce)
        assertEquals(nonce, assertNotNull(commands.pending("subscription-service")).nonce)

        commands.markDelivered("subscription-service", nonce, TEST_TIME)

        assertNull(commands.pending("subscription-service"), "повторно та же команда не выдаётся")

        val second = commands.issue("subscription-service", "restart", "operator", TEST_TIME)
        assertEquals(2, second, "следующее нажатие выпускает новую команду")
    }

    @Test
    fun `повторный отчёт не принимается дважды`() {
        assertTrue(reports.claim("node-1", "batch-1", TEST_TIME))
        assertFalse(reports.claim("node-1", "batch-1", TEST_TIME), "дубликат обязан отсекаться")
        assertTrue(reports.claim("node-1", "batch-2", TEST_TIME))
    }

    @Test
    fun `удаление агента уносит его состояние, команды и отчёты`() {
        statuses.record(AgentStatus(agentId = "node-1", lastReportAt = TEST_TIME))
        commands.issue("node-1", "restart", "operator", TEST_TIME)
        reports.claim("node-1", "batch-1", TEST_TIME)

        PostgresSupport.jdbc.update("DELETE FROM agents WHERE id = 'node-1'")

        assertNull(statuses.find("node-1"))
        assertNull(commands.pending("node-1"))
        assertEquals(
            0,
            PostgresSupport.jdbc.queryForObject(
                "SELECT count(*) FROM agent_reports WHERE agent_id = 'node-1'",
                Int::class.java,
            ),
        )
    }
}
