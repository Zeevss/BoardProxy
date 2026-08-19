package io.boardproxy.control.delivery.application

import com.fasterxml.jackson.module.kotlin.readValue
import io.boardproxy.control.delivery.infrastructure.persistence.postgres.PostgresNodeRuntimeSink
import io.boardproxy.control.delivery.infrastructure.persistence.postgres.PostgresNodeTrafficSink
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresNodeRepository
import io.boardproxy.control.shared.agents.postgres.PostgresAgentCommandRepository
import io.boardproxy.control.shared.agents.postgres.PostgresAgentReportLog
import io.boardproxy.control.shared.agents.postgres.PostgresAgentStatusRepository
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.testing.PostgresSupport
import io.boardproxy.control.testing.TEST_TIME
import io.boardproxy.control.testing.testNode
import java.time.Clock
import java.time.ZoneOffset
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class NodeReportServiceTest {
    private val nodes = PostgresNodeRepository(PostgresSupport.named, PostgresSupport.json, PostgresSupport.cipher)
    private val statuses = PostgresAgentStatusRepository(PostgresSupport.named, PostgresSupport.json)
    private val commands = PostgresAgentCommandRepository(PostgresSupport.named)

    private val service = NodeReportService(
        reports = PostgresAgentReportLog(PostgresSupport.named),
        statuses = statuses,
        commands = commands,
        traffic = PostgresNodeTrafficSink(PostgresSupport.named),
        runtime = PostgresNodeRuntimeSink(PostgresSupport.named, PostgresSupport.json),
        transactions = object : TransactionRunner {
            override fun <T> required(block: () -> T): T = block()
        },
        clock = Clock.fixed(TEST_TIME, ZoneOffset.UTC),
    )

    @BeforeTest
    fun prepare() {
        assertTrue(PostgresSupport.dockerAvailable, "тесты приёма отчётов требуют Docker")
        PostgresSupport.truncate()
        nodes.create(testNode("node-1"))
    }

    /** Главная гарантия приёма: повтор не должен удваивать трафик. */
    @Test
    fun `повтор отчёта не удваивает трафик`() {
        service.accept(report(batchId = "batch-1"))
        service.accept(report(batchId = "batch-1"))

        assertEquals(150, userTraffic())
        assertEquals(1, deltaRows("user_traffic_deltas"))
        assertEquals(1, deltaRows("interface_traffic_deltas"))
    }

    @Test
    fun `новый batch_id учитывается`() {
        service.accept(report(batchId = "batch-1"))
        service.accept(report(batchId = "batch-2"))

        assertEquals(300, userTraffic())
    }

    /** Состояние обновляется и на повторе: оно идемпотентно и отражает «сейчас». */
    @Test
    fun `состояние ноды пишется из отчёта`() {
        service.accept(report(batchId = "batch-1", appliedRevision = 7))

        val status = assertNotNull(statuses.find("node-1"))

        assertEquals(7, status.appliedRevision)
        assertEquals("boot-1", status.bootId)
        assertEquals(3, status.seq)
        assertEquals(TEST_TIME, status.lastReportAt)
        assertTrue(status.online(TEST_TIME.plusSeconds(10)))
        assertNull(status.applyError, "пустая ошибка означает успешное применение")
    }

    @Test
    fun `ошибка применения сохраняется`() {
        service.accept(report(batchId = "batch-1", applyError = "toml parse failed"))

        assertEquals("toml parse failed", assertNotNull(statuses.find("node-1")).applyError)
    }

    @Test
    fun `команда отдаётся ровно один раз`() {
        commands.issue("node-1", "restart", "operator", TEST_TIME)

        val first = service.accept(report(batchId = "batch-1"))
        val second = service.accept(report(batchId = "batch-2"))

        assertEquals(listOf("restart"), first.map { it.kind })
        assertTrue(second.isEmpty(), "перезапуск не должен выдаваться повторно")
    }

    /** Снимок заменяется целиком — проекции по событиям больше нет. */
    @Test
    fun `runtime-снимок заменяется целиком`() {
        service.accept(report(batchId = "batch-1", sessions = 2))
        service.accept(report(batchId = "batch-2", sessions = 5))

        val stored = PostgresSupport.jdbc.queryForObject(
            "SELECT snapshot::text FROM node_runtime WHERE node_id = 'node-1'",
            String::class.java,
        )
        val snapshot = PostgresSupport.json.readValue<Map<String, Any>>(assertNotNull(stored))

        @Suppress("UNCHECKED_CAST")
        val users = snapshot["users"] as List<Map<String, Any>>
        assertEquals(1, users.size, "снимок должен заменяться, а не накапливаться")
        assertEquals(5, users.single()["activeSessions"])
        assertEquals(
            1,
            PostgresSupport.jdbc.queryForObject("SELECT count(*) FROM node_runtime", Int::class.java),
        )
    }

    @Test
    fun `события копятся журналом`() {
        service.accept(report(batchId = "batch-1"))
        service.accept(report(batchId = "batch-2"))

        assertEquals(
            2,
            PostgresSupport.jdbc.queryForObject("SELECT count(*) FROM runtime_events", Int::class.java),
        )
    }

    private fun report(
        batchId: String,
        appliedRevision: Long = 1,
        applyError: String = "",
        sessions: Int = 1,
    ) = NodeReport(
        nodeId = "node-1",
        bootId = "boot-1",
        seq = 3,
        batchId = batchId,
        appliedRevision = appliedRevision,
        appliedSha256 = "a".repeat(64),
        applyError = applyError,
        coreVersion = "1.2.3",
        agentVersion = "0.9.0",
        uptimeSeconds = 42,
        observedAt = TEST_TIME,
        runtime = RuntimeSnapshotInput(
            coreBootId = "boot-1",
            capturedAt = TEST_TIME,
            users = listOf(RuntimeUserInput("user-1", sessions, 1, TEST_TIME)),
            boards = listOf(RuntimeBoardInput("board-1", "connected", 1, "")),
        ),
        interfaceTraffic = listOf(
            InterfaceTrafficInput("eth0", 10, 20, 1, 1, 0, 0, 0, 0, TEST_TIME),
        ),
        userTraffic = listOf(UserTrafficInput("user-1", 100, 50, TEST_TIME)),
        events = listOf(RuntimeEventInput("session.opened", TEST_TIME, """{"userId":"user-1"}""")),
    )

    private fun userTraffic(): Long = PostgresSupport.jdbc.queryForObject(
        "SELECT COALESCE(SUM(rx_bytes + tx_bytes), 0) FROM user_traffic_deltas",
        Long::class.java,
    ) ?: 0

    private fun deltaRows(table: String): Int =
        PostgresSupport.jdbc.queryForObject("SELECT count(*) FROM $table", Int::class.java) ?: 0
}
