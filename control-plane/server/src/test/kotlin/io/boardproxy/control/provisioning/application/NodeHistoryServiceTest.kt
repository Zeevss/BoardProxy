package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Grant
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.infrastructure.compiler.toml.TomlCoreConfigCompiler
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresBoardRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresDesiredConfigRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresGrantRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresNodeRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresNodeSnapshotRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresUserRepository
import io.boardproxy.control.shared.contracts.QuotaExceededQueries
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.testing.PostgresSupport
import io.boardproxy.control.testing.TEST_TIME
import io.boardproxy.control.testing.testBoard
import io.boardproxy.control.testing.testNode
import io.boardproxy.control.testing.testUser
import java.time.Clock
import java.time.ZoneOffset
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertContains
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * История проверяется на живой базе: откат затрагивает четыре таблицы сразу,
 * и заменять их фейками означало бы проверять фейки.
 */
class NodeHistoryServiceTest {
    private val nodes = PostgresNodeRepository(PostgresSupport.named, PostgresSupport.json, PostgresSupport.cipher)
    private val boards = PostgresBoardRepository(PostgresSupport.named)
    private val users = PostgresUserRepository(PostgresSupport.named, PostgresSupport.cipher)
    private val grants = PostgresGrantRepository(PostgresSupport.named)
    private val configs = PostgresDesiredConfigRepository(PostgresSupport.named, PostgresSupport.cipher)
    private val snapshots =
        PostgresNodeSnapshotRepository(PostgresSupport.named, PostgresSupport.json, PostgresSupport.cipher)

    private val transactions = object : TransactionRunner {
        override fun <T> required(block: () -> T): T = block()
    }
    private val states = NodeStateService(nodes, boards, users, grants)
    private val publisher = DesiredConfigPublisher(
        states = states,
        quotas = QuotaExceededQueries { emptySet() },
        compiler = TomlCoreConfigCompiler(),
        configs = configs,
        snapshots = snapshots,
        audit = { },
        outbox = { },
        clock = Clock.fixed(TEST_TIME, ZoneOffset.UTC),
    )
    private val history = NodeHistoryService(
        snapshots, nodes, boards, users, grants, publisher, transactions,
        Clock.fixed(TEST_TIME, ZoneOffset.UTC),
    )

    @BeforeTest
    fun prepare() {
        assertTrue(PostgresSupport.dockerAvailable, "тесты истории требуют Docker")
        PostgresSupport.truncate()
        nodes.create(testNode("node-1"))
        boards.create(testBoard("node-1", "board-1", "hash-1"))
        users.create(testUser("user-1"))
        grants.replace("user-1", listOf(Grant("user-1", "node-1", setOf("board-1"))))
        publisher.publish(setOf("node-1"), "node.created", "operator")
    }

    @Test
    fun `история отдаёт снимки от свежих к старым`() {
        boards.create(testBoard("node-1", "board-2", "hash-2"))
        publisher.publish(setOf("node-1"), "board.added", "operator")

        val page = history.history("node-1", 0, 10)

        assertEquals(2, page.total)
        assertEquals(listOf(2L, 1L), page.items.map { it.seq })
        assertEquals("board.added", page.items.first().cause)
        assertEquals("operator", page.items.first().actor)
    }

    @Test
    fun `diff показывает только изменившееся и не раскрывает ключи`() {
        boards.replace(testBoard("node-1", "board-1", "hash-1", state = ResourceState.DISABLED, version = 2), 1)
        publisher.publish(setOf("node-1"), "board.disabled", "operator")

        val diff = history.diff("node-1", 1, 2)

        assertEquals(
            listOf("boards.board-1.state"),
            diff.changes.map { it.path },
            "имя ноды и лимиты не менялись — их в diff быть не должно",
        )
        assertEquals("enabled", diff.changes.single().before)
        assertEquals("disabled", diff.changes.single().after)
        assertTrue(diff.changes.none { (it.before.orEmpty() + it.after.orEmpty()).contains("base64:") })
    }

    @Test
    fun `откат восстанавливает борды и размещения`() {
        boards.create(testBoard("node-1", "board-2", "hash-2"))
        grants.replace("user-1", listOf(Grant("user-1", "node-1", setOf("board-1", "board-2"))))
        publisher.publish(setOf("node-1"), "board.added", "operator")

        val result = history.rollback("node-1", 1, "operator")

        assertTrue(result.changed)
        assertNull(boards.find("node-1", "board-2"), "борд, которого не было в снимке, удаляется")
        assertEquals(setOf("board-1"), grants.of("user-1").single().boardIds)
    }

    /** Ревизия идёт вперёд: откат — это обычная правка, а не путешествие назад. */
    @Test
    fun `откат двигает ревизию вперёд`() {
        val before = assertNotNull(configs.find("node-1")).revision
        boards.create(testBoard("node-1", "board-2", "hash-2"))
        publisher.publish(setOf("node-1"), "board.added", "operator")

        history.rollback("node-1", 1, "operator")

        assertEquals(before + 2, assertNotNull(configs.find("node-1")).revision)
    }

    /**
     * Пользователь флотовый: откат конфигурации одной ноды не имеет права
     * трогать его размещения на других нодах.
     */
    @Test
    fun `откат не затрагивает размещения на других нодах`() {
        nodes.create(testNode("node-2"))
        boards.create(testBoard("node-2", "board-9", "hash-9"))
        grants.replace(
            "user-1",
            listOf(
                Grant("user-1", "node-1", setOf("board-1")),
                Grant("user-1", "node-2", setOf("board-9")),
            ),
        )
        publisher.publish(setOf("node-1"), "grants.changed", "operator")

        history.rollback("node-1", 1, "operator")

        assertEquals(setOf("node-1", "node-2"), grants.nodesOf("user-1"))
        assertEquals(setOf("board-9"), grants.of("user-1").single { it.nodeId == "node-2" }.boardIds)
    }

    /** Откат конфигурации ноды не должен воскрешать удалённого из флота человека. */
    @Test
    fun `откат пропускает размещение удалённого пользователя`() {
        users.delete("user-1")
        publisher.publish(setOf("node-1"), "user.deleted", "operator")

        val result = history.rollback("node-1", 1, "operator")

        assertNull(users.find("user-1"))
        assertTrue(grants.onNode("node-1").isEmpty())
        assertFalse(result.configSha256.isBlank())
    }

    @Test
    fun `откат восстанавливает настройки ноды`() {
        nodes.replace(testNode("node-1", name = "Renamed", state = ResourceState.DISABLED, version = 2), 1)
        publisher.publish(setOf("node-1"), "node.updated", "operator")

        history.rollback("node-1", 1, "operator")

        val restored = assertNotNull(nodes.find("node-1"))
        assertEquals("Node One", restored.name)
        assertEquals(ResourceState.ENABLED, restored.state)
    }

    @Test
    fun `несуществующий снимок и несуществующая нода отвергаются`() {
        assertFailsWith<ResourceNotFound> { history.rollback("node-1", 99, "operator") }
        assertFailsWith<ResourceNotFound> { history.history("unknown", 0, 10) }
        val error = assertFailsWith<ResourceNotFound> { history.diff("node-1", 1, 99) }
        assertContains(error.message.orEmpty(), "snapshot")
    }
}

private fun assertFalse(value: Boolean) = assertTrue(!value)
