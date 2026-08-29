package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresNodeRepository
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.telemetry.application.TrafficKind
import io.boardproxy.control.testing.PostgresSupport
import io.boardproxy.control.testing.TEST_TIME
import io.boardproxy.control.testing.testNode
import java.time.Duration
import java.time.Instant
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Запросы трафика переписаны под новую схему: дельты несут собственный
 * observed_at и agent_id, соединение с таблицей батчей исчезло вместе с ней.
 * Рукописный SQL это не проверяет — проверяет только живая база.
 */
class PostgresTrafficQueriesTest {
    private val nodes = PostgresNodeRepository(PostgresSupport.named, PostgresSupport.json, PostgresSupport.cipher)
    private val queries = PostgresTrafficQueries(PostgresSupport.named)
    private val maintenance = PostgresTrafficMaintenance(PostgresSupport.named)

    @BeforeTest
    fun prepare() {
        assertTrue(PostgresSupport.dockerAvailable, "тесты трафика требуют Docker")
        PostgresSupport.truncate()
        nodes.create(testNode("node-1"))
    }

    @Test
    fun `тоталы интерфейсов и пользователей считаются в окне`() {
        record("batch-1", TEST_TIME, iface = "eth0", user = "user-1", rx = 100, tx = 50)
        record("batch-2", TEST_TIME.plusSeconds(60), iface = "eth0", user = "user-1", rx = 10, tx = 5)
        record("batch-3", TEST_TIME.minus(Duration.ofDays(2)), iface = "eth0", user = "user-1", rx = 999, tx = 999)

        val from = TEST_TIME.minusSeconds(1)
        val to = TEST_TIME.plusSeconds(120)

        val interfaces = queries.interfaceTotals("node-1", from, to).single()
        assertEquals(110, interfaces.rxBytes)
        assertEquals(55, interfaces.txBytes)

        val users = queries.userTotals("node-1", from, to).single()
        assertEquals("user-1", users.subject)
        assertEquals(110, users.rxBytes)
    }

    @Test
    fun `ряд раскладывается по корзинам`() {
        record("batch-1", TEST_TIME, iface = "eth0", user = "user-1", rx = 10, tx = 0)
        record("batch-2", TEST_TIME.plusSeconds(30), iface = "eth0", user = "user-1", rx = 20, tx = 0)
        record("batch-3", TEST_TIME.plusSeconds(3600), iface = "eth0", user = "user-1", rx = 5, tx = 0)

        val points = queries.series(
            "node-1", TrafficKind.USER,
            TEST_TIME.minusSeconds(1), TEST_TIME.plusSeconds(7200), bucketSeconds = 3600,
        )

        assertEquals(2, points.size, "две часовые корзины")
        assertEquals(30, points.first().rxBytes)
        assertEquals(5, points.last().rxBytes)
    }

    @Test
    fun `почасовые роллапы строятся из дельт`() {
        record("batch-1", TEST_TIME, iface = "eth0", user = "user-1", rx = 100, tx = 50)

        val rows = maintenance.rebuildHourly(TEST_TIME.minusSeconds(3600), TEST_TIME.plusSeconds(3600))

        assertTrue(rows > 0, "роллапы должны появиться")
        assertEquals(
            150,
            PostgresSupport.jdbc.queryForObject(
                "SELECT SUM(rx_bytes + tx_bytes) FROM traffic_hourly_rollups WHERE traffic_kind = 'user'",
                Long::class.java,
            ),
        )
    }

    @Test
    fun `готовый роллап не считается второй раз вместе с raw`() {
        record("batch-1", TEST_TIME.minusSeconds(7200), iface = "eth0", user = "user-1", rx = 100, tx = 50)
        maintenance.rebuildHourly(TEST_TIME.minusSeconds(10800), TEST_TIME.minusSeconds(3600))

        val total = queries.userTotals(
            "node-1", TEST_TIME.minusSeconds(10800), TEST_TIME.minusSeconds(3600),
        ).single()

        assertEquals(100, total.rxBytes)
        assertEquals(50, total.txBytes)
    }

    @Test
    fun `история читается из роллапа после удаления raw`() {
        val old = TEST_TIME.minus(Duration.ofDays(40))
        record("batch-old", old, iface = "eth0", user = "user-1", rx = 100, tx = 50)
        maintenance.rebuildHourly(old.minusSeconds(3600), old.plusSeconds(3600))
        maintenance.deleteRawBefore(TEST_TIME.minus(Duration.ofDays(31)))

        val total = queries.userTotals("node-1", old.minusSeconds(3600), old.plusSeconds(3600)).single()

        assertEquals(150, total.rxBytes + total.txBytes)
    }

    /** Retention чистит отчёты; дельты уходят каскадом вместе с ними. */
    @Test
    fun `удаление старых отчётов уносит дельты`() {
        record("batch-old", TEST_TIME.minus(Duration.ofDays(40)), iface = "eth0", user = "user-1", rx = 1, tx = 1)
        record("batch-new", TEST_TIME, iface = "eth0", user = "user-1", rx = 2, tx = 2)

        maintenance.deleteRawBefore(TEST_TIME.minus(Duration.ofDays(31)))

        assertEquals(
            1,
            PostgresSupport.jdbc.queryForObject("SELECT count(*) FROM user_traffic_deltas", Int::class.java),
        )
    }

    private fun record(batchId: String, at: Instant, iface: String, user: String, rx: Long, tx: Long) {
        PostgresSupport.jdbc.update(
            "INSERT INTO agent_reports (agent_id, batch_id, received_at) VALUES ('node-1', ?, ?)",
            batchId, at.toSqlTimestamp(),
        )
        PostgresSupport.jdbc.update(
            """
            INSERT INTO interface_traffic_deltas (
                agent_id, batch_id, interface_name, rx_bytes, tx_bytes, rx_packets,
                tx_packets, rx_errors, tx_errors, rx_dropped, tx_dropped, observed_at
            ) VALUES ('node-1', ?, ?, ?, ?, 0, 0, 0, 0, 0, 0, ?)
            """.trimIndent(),
            batchId, iface, rx, tx, at.toSqlTimestamp(),
        )
        PostgresSupport.jdbc.update(
            """
            INSERT INTO user_traffic_deltas (agent_id, batch_id, user_id, rx_bytes, tx_bytes, observed_at)
            VALUES ('node-1', ?, ?, ?, ?, ?)
            """.trimIndent(),
            batchId, user, rx, tx, at.toSqlTimestamp(),
        )
    }
}
