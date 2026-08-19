package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresBoardRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresNodeRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresUserRepository
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import io.boardproxy.control.telemetry.domain.TrafficQuota
import io.boardproxy.control.telemetry.domain.TrafficQuotaState
import io.boardproxy.control.testing.PostgresSupport
import io.boardproxy.control.testing.TEST_TIME
import io.boardproxy.control.testing.testBoard
import io.boardproxy.control.testing.testNode
import io.boardproxy.control.testing.testUser
import java.time.Instant
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

class PostgresTrafficQuotaRepositoryTest {
    private val nodes = PostgresNodeRepository(PostgresSupport.named, PostgresSupport.json, PostgresSupport.cipher)
    private val boards = PostgresBoardRepository(PostgresSupport.named)
    private val users = PostgresUserRepository(PostgresSupport.named, PostgresSupport.cipher)
    private val quotas = PostgresTrafficQuotaRepository(PostgresSupport.named)
    private val usage = PostgresUserUsageQueries(PostgresSupport.named)

    @BeforeTest
    fun prepare() {
        assertTrue(PostgresSupport.dockerAvailable, "тесты репозиториев требуют Docker")
        PostgresSupport.truncate()
        nodes.create(testNode("node-1"))
        nodes.create(testNode("node-2"))
        boards.create(testBoard("node-1", "board-1", "hash-1"))
        boards.create(testBoard("node-2", "board-2", "hash-2"))
        users.create(testUser("user-1"))
    }

    @Test
    fun `квота переживает круг записи и чтения`() {
        val quota = quota()

        assertTrue(quotas.save(quota, expectedVersion = null))

        assertEquals(quota, quotas.find("user-1"))
        assertEquals(listOf("user-1"), quotas.list().map { it.userId })
        assertEquals(listOf("user-1"), quotas.enabled().map { it.userId })
    }

    @Test
    fun `замена квоты защищена версией`() {
        quotas.save(quota(), expectedVersion = null)

        assertFalse(quotas.save(quota(limit = 500).copy(version = 2), expectedVersion = 99))
        assertTrue(quotas.save(quota(limit = 500).copy(version = 2), expectedVersion = 1))
        assertEquals(500, quotas.find("user-1")?.limitBytes)
    }

    /** Флотовость квоты: расход складывается по всем нодам, где размещён пользователь. */
    @Test
    fun `расход суммируется по всем нодам`() {
        recordTraffic("node-1", "batch-1", rx = 100, tx = 50)
        recordTraffic("node-2", "batch-2", rx = 200, tx = 25)

        assertEquals(375, quotas.usedBytes("user-1", Instant.EPOCH, TEST_TIME.plusSeconds(60)))
    }

    @Test
    fun `окно отсчёта ограничивает расход`() {
        recordTraffic("node-1", "batch-1", rx = 100, tx = 0, at = TEST_TIME.minusSeconds(3600))
        recordTraffic("node-1", "batch-2", rx = 10, tx = 0, at = TEST_TIME)

        assertEquals(10, quotas.usedBytes("user-1", TEST_TIME.minusSeconds(60), TEST_TIME.plusSeconds(60)))
    }

    @Test
    fun `смена флага превышения различается от простого обновления расхода`() {
        quotas.save(quota(), expectedVersion = null)

        assertTrue(quotas.saveState(state(used = 10, exceeded = false)), "первая запись — изменение")
        assertFalse(quotas.saveState(state(used = 20, exceeded = false)), "рост расхода флаг не меняет")
        assertTrue(quotas.saveState(state(used = 200, exceeded = true)), "смена флага — изменение")
        assertFalse(quotas.saveState(state(used = 300, exceeded = true)))

        assertEquals(setOf("user-1"), quotas.exceededUsers())
        assertEquals(300, assertNotNull(quotas.state("user-1")).usedBytes)

        assertTrue(quotas.saveState(state(used = 0, exceeded = false)), "снятие превышения — тоже изменение")
        assertEquals(emptySet(), quotas.exceededUsers())
    }

    @Test
    fun `сброс счётчика сдвигает начало отсчёта`() {
        quotas.save(quota(), expectedVersion = null)

        quotas.startNewCounter("user-1", TEST_TIME)

        assertEquals(TEST_TIME, quotas.find("user-1")?.counterStart)
    }

    @Test
    fun `расход пользователя разбивается по нодам вместе с лимитом`() {
        quotas.save(quota(limit = 1_000), expectedVersion = null)
        recordTraffic("node-1", "batch-1", rx = 100, tx = 50)
        recordTraffic("node-2", "batch-2", rx = 200, tx = 25)

        val result = usage.usage("user-1")

        assertEquals(1_000, result.limitBytes)
        assertEquals(375, result.usedBytes)
        assertEquals(mapOf("node-1" to 150L, "node-2" to 225L), result.perNode)
    }

    @Test
    fun `удаление пользователя уносит квоту и её состояние`() {
        quotas.save(quota(), expectedVersion = null)
        quotas.saveState(state(used = 10, exceeded = false))

        users.delete("user-1")

        assertEquals(null, quotas.find("user-1"))
        assertEquals(emptySet(), quotas.exceededUsers())
    }

    private fun quota(limit: Long = 100) = TrafficQuota(
        userId = "user-1", period = QuotaPeriod.MONTHLY, limitBytes = limit,
        action = QuotaAction.DISABLE, enabled = true, version = 1, updatedAt = TEST_TIME,
    )

    private fun state(used: Long, exceeded: Boolean) =
        TrafficQuotaState("user-1", TEST_TIME, used, exceeded, TEST_TIME)

    private fun recordTraffic(nodeId: String, batchId: String, rx: Long, tx: Long, at: Instant = TEST_TIME) {
        PostgresSupport.jdbc.update(
            "INSERT INTO agent_reports (agent_id, batch_id) VALUES (?, ?)",
            nodeId, batchId,
        )
        PostgresSupport.jdbc.update(
            """
            INSERT INTO user_traffic_deltas (agent_id, batch_id, user_id, rx_bytes, tx_bytes, observed_at)
            VALUES (?, ?, 'user-1', ?, ?, ?)
            """.trimIndent(),
            nodeId, batchId, rx, tx, at.toSqlTimestamp(),
        )
    }
}
