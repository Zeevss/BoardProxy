package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.application.DesiredConfig
import io.boardproxy.control.provisioning.domain.model.Grant
import io.boardproxy.control.provisioning.domain.model.NodeState
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.UserPlacement
import io.boardproxy.control.testing.PostgresSupport
import io.boardproxy.control.testing.TEST_TIME
import io.boardproxy.control.testing.testBoard
import io.boardproxy.control.testing.testKey
import io.boardproxy.control.testing.testNode
import io.boardproxy.control.testing.testUser
import java.time.Duration
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Доступ к данным рукописный, поэтому расхождение SQL и схемы компилятор не
 * поймает. Эти тесты выполняют каждый запрос на живой базе — это и есть
 * компенсация за отказ от ORM.
 */
class PostgresProvisioningRepositoriesTest {
    private val nodes = PostgresNodeRepository(PostgresSupport.named, PostgresSupport.json, PostgresSupport.cipher)
    private val boards = PostgresBoardRepository(PostgresSupport.named)
    private val users = PostgresUserRepository(PostgresSupport.named, PostgresSupport.cipher)
    private val grants = PostgresGrantRepository(PostgresSupport.named)
    private val configs = PostgresDesiredConfigRepository(PostgresSupport.named, PostgresSupport.cipher)
    private val snapshots =
        PostgresNodeSnapshotRepository(PostgresSupport.named, PostgresSupport.json, PostgresSupport.cipher)

    @BeforeTest
    fun prepare() {
        assertTrue(PostgresSupport.dockerAvailable, "тесты репозиториев требуют Docker")
        PostgresSupport.truncate()
    }

    // --- ноды ---

    @Test
    fun `нода переживает круг записи и чтения вместе с зашифрованным ключом`() {
        val node = testNode().let {
            it.copy(core = it.core.copy(server = it.core.server.copy(idleTimeout = Duration.ofSeconds(45))))
        }
        nodes.create(node)

        val loaded = assertNotNull(nodes.find("node-1"))

        assertEquals(node, loaded)
        assertEquals(testKey(1), loaded.core.server.privateKey, "приватный ключ должен расшифровываться обратно")
        assertEquals(Duration.ofSeconds(45), loaded.core.server.idleTimeout)
    }

    @Test
    fun `приватный ключ ноды не лежит в базе открытым текстом`() {
        nodes.create(testNode())

        val stored = PostgresSupport.jdbc.queryForObject(
            "SELECT encode(server_key_ciphertext, 'escape') FROM nodes WHERE id = 'node-1'",
            String::class.java,
        )

        assertFalse(stored!!.contains("base64:"), "в шифртексте не должно быть исходного ключа")
    }

    @Test
    fun `замена ноды защищена версией`() {
        nodes.create(testNode())

        assertFalse(nodes.replace(testNode(name = "Renamed", version = 2), expectedVersion = 99))
        assertTrue(nodes.replace(testNode(name = "Renamed", version = 2), expectedVersion = 1))
        assertEquals("Renamed", nodes.find("node-1")?.name)
    }

    @Test
    fun `поиск нод фильтрует и считает`() {
        nodes.create(testNode("node-1", "Alpha"))
        nodes.create(testNode("node-2", "Beta"))

        assertEquals(2, nodes.count(null))
        assertEquals(1, nodes.count("Alpha"))
        assertEquals(listOf("node-1"), nodes.list("Alpha", 0, 10).map { it.id })
        assertEquals(listOf("node-2"), nodes.list(null, 1, 10).map { it.id })
    }

    // --- борды ---

    @Test
    fun `борд переживает круг записи и чтения`() {
        nodes.create(testNode())
        val board = testBoard().copy(hubSlide = "slide-1", apiBase = "https://example.test/api", guestName = "guest")
        boards.create(board)

        assertEquals(board, boards.find("node-1", "board-1"))
        assertEquals(listOf("board-1"), boards.listByNode("node-1").map { it.id })
    }

    @Test
    fun `борды фильтруются по ноде и запросу`() {
        nodes.create(testNode("node-1"))
        nodes.create(testNode("node-2"))
        boards.create(testBoard("node-1", "alpha", "hash-a"))
        boards.create(testBoard("node-2", "beta", "hash-b"))

        assertEquals(2, boards.count(null, null))
        assertEquals(1, boards.count(null, "node-1"))
        assertEquals(listOf("beta"), boards.list("hash-b", null, 0, 10).map { it.id })
    }

    @Test
    fun `замена борда защищена версией`() {
        nodes.create(testNode())
        boards.create(testBoard())

        assertFalse(boards.replace(testBoard(version = 2), expectedVersion = 99))
        assertTrue(boards.replace(testBoard(state = ResourceState.DISABLED, version = 2), expectedVersion = 1))
        assertEquals(ResourceState.DISABLED, boards.find("node-1", "board-1")?.state)
    }

    // --- пользователи ---

    @Test
    fun `пользователь переживает круг записи и чтения вместе с ключом`() {
        val user = testUser()
        users.create(user)

        val loaded = assertNotNull(users.find("user-1"))

        assertEquals(user, loaded)
        assertEquals(testKey(2), loaded.privateKey)
        assertEquals(user.identityFingerprint(), assertNotNull(users.findByFingerprint(user.identityFingerprint())).identityFingerprint())
    }

    /** Флотовость: отбор по ноде идёт через гранты, а не через копию пользователя. */
    @Test
    fun `пользователи фильтруются по ноде через гранты`() {
        nodes.create(testNode("node-1"))
        nodes.create(testNode("node-2"))
        boards.create(testBoard("node-1", "board-1", "hash-1"))
        boards.create(testBoard("node-2", "board-2", "hash-2"))
        users.create(testUser("user-1", keyByte = 2))
        users.create(testUser("user-2", keyByte = 3))
        grants.replace("user-1", listOf(Grant("user-1", "node-1", setOf("board-1"))))
        grants.replace("user-2", listOf(Grant("user-2", "node-2", setOf("board-2"))))

        assertEquals(2, users.count(null, null))
        assertEquals(listOf("user-1"), users.list(null, "node-1", 0, 10).map { it.id })
        assertEquals(listOf("user-2"), users.list(null, "node-2", 0, 10).map { it.id })
    }

    @Test
    fun `удаление пользователя уносит его гранты`() {
        nodes.create(testNode())
        boards.create(testBoard())
        users.create(testUser())
        grants.replace("user-1", listOf(Grant("user-1", "node-1", setOf("board-1"))))

        assertTrue(users.delete("user-1"))

        assertNull(users.find("user-1"))
        assertTrue(grants.onNode("node-1").isEmpty())
    }

    // --- гранты ---

    @Test
    fun `гранты группируются по ноде и заменяются целиком`() {
        nodes.create(testNode("node-1"))
        nodes.create(testNode("node-2"))
        boards.create(testBoard("node-1", "board-1", "hash-1"))
        boards.create(testBoard("node-1", "board-2", "hash-2"))
        boards.create(testBoard("node-2", "board-3", "hash-3"))
        users.create(testUser())

        grants.replace(
            "user-1",
            listOf(
                Grant("user-1", "node-1", setOf("board-1", "board-2")),
                Grant("user-1", "node-2", setOf("board-3")),
            ),
        )

        assertEquals(setOf("node-1", "node-2"), grants.nodesOf("user-1"))
        assertEquals(setOf("board-1", "board-2"), grants.of("user-1").first { it.nodeId == "node-1" }.boardIds)
        assertEquals(listOf("user-1"), grants.onNode("node-2").map { it.userId })

        grants.replace("user-1", listOf(Grant("user-1", "node-2", setOf("board-3"))))

        assertEquals(setOf("node-2"), grants.nodesOf("user-1"), "замена целиком должна убрать прежние размещения")
    }

    // --- конфигурация ---

    @Test
    fun `конфигурация перезаписывается на месте и расшифровывается обратно`() {
        nodes.create(testNode())
        val toml = "version = 1\n".toByteArray()
        configs.save(DesiredConfig("node-1", 1, "a".repeat(64), toml, TEST_TIME))

        val first = assertNotNull(configs.find("node-1"))
        assertEquals(1, first.revision)
        assertEquals(toml.decodeToString(), first.configToml.decodeToString())

        val next = "version = 1\n# changed\n".toByteArray()
        configs.save(DesiredConfig("node-1", 2, "b".repeat(64), next, TEST_TIME))

        val second = assertNotNull(configs.find("node-1"))
        assertEquals(2, second.revision, "хранится только текущая конфигурация")
        assertEquals(next.decodeToString(), second.configToml.decodeToString())
        assertEquals(
            1,
            PostgresSupport.jdbc.queryForObject("SELECT count(*) FROM node_desired_config", Int::class.java),
        )
    }

    // --- снимки ---

    @Test
    fun `снимок восстанавливает владеемое состояние целиком`() {
        nodes.create(testNode())
        val state = NodeState(
            node = testNode(),
            boards = listOf(testBoard()),
            placements = listOf(UserPlacement(testUser(), setOf("board-1"))),
        )

        assertEquals(1, snapshots.save(state, "node.created", "operator", TEST_TIME))
        assertEquals(2, snapshots.save(state, "node.updated", "operator", TEST_TIME), "seq должен расти")

        val restored = assertNotNull(snapshots.find("node-1", 1))

        assertEquals(state, restored)
        assertEquals(testKey(1), restored.node.core.server.privateKey)
        assertEquals(testKey(2), restored.placements.single().user.privateKey)
        assertEquals(2, snapshots.count("node-1"))
        assertEquals(listOf(2L, 1L), snapshots.list("node-1", 0, 10).map { it.seq })
    }

    @Test
    fun `удаление ноды уносит конфигурацию и снимки`() {
        nodes.create(testNode())
        configs.save(DesiredConfig("node-1", 1, "a".repeat(64), "x".toByteArray(), TEST_TIME))
        snapshots.save(NodeState(testNode(), emptyList(), emptyList()), "node.created", "operator", TEST_TIME)

        assertTrue(nodes.delete("node-1"))

        assertNull(configs.find("node-1"))
        assertEquals(0, snapshots.count("node-1"))
    }
}
