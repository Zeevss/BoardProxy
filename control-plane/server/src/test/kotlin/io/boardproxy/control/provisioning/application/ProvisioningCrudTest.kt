package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.infrastructure.compiler.toml.TomlCoreConfigCompiler
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresBoardRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresDesiredConfigRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresGrantRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresNodeRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresNodeSnapshotRepository
import io.boardproxy.control.provisioning.infrastructure.persistence.postgres.PostgresUserRepository
import io.boardproxy.control.shared.contracts.QuotaExceededQueries
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.testing.PostgresSupport
import io.boardproxy.control.testing.TEST_TIME
import io.boardproxy.control.testing.testKey
import java.time.Clock
import java.time.ZoneOffset
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class ProvisioningCrudTest {
    private val nodesRepo = PostgresNodeRepository(PostgresSupport.named, PostgresSupport.json, PostgresSupport.cipher)
    private val boardsRepo = PostgresBoardRepository(PostgresSupport.named)
    private val usersRepo = PostgresUserRepository(PostgresSupport.named, PostgresSupport.cipher)
    private val grantsRepo = PostgresGrantRepository(PostgresSupport.named)
    private val configs = PostgresDesiredConfigRepository(PostgresSupport.named, PostgresSupport.cipher)
    private val snapshots =
        PostgresNodeSnapshotRepository(PostgresSupport.named, PostgresSupport.json, PostgresSupport.cipher)

    private val clock = Clock.fixed(TEST_TIME, ZoneOffset.UTC)
    private val auditEvents = mutableListOf<AuditEvent>()
    private val audit = AuditRepository { auditEvents += it }
    private val transactions = object : TransactionRunner {
        override fun <T> required(block: () -> T): T = block()
    }
    private val publisher = DesiredConfigPublisher(
        states = NodeStateService(nodesRepo, boardsRepo, usersRepo, grantsRepo),
        quotas = QuotaExceededQueries { emptySet() },
        compiler = TomlCoreConfigCompiler(),
        configs = configs,
        snapshots = snapshots,
        audit = { },
        outbox = { },
        clock = clock,
    )
    private val nodes = NodeService(nodesRepo, publisher, transactions, clock, audit)
    private val boards = BoardService(boardsRepo, nodesRepo, publisher, transactions, clock, audit)
    private val users = UserService(usersRepo, boardsRepo, grantsRepo, publisher, transactions, clock, audit)

    @BeforeTest
    fun prepare() {
        assertTrue(PostgresSupport.dockerAvailable, "тесты CRUD требуют Docker")
        PostgresSupport.truncate()
        auditEvents.clear()
    }

    // --- ноды ---

    @Test
    fun `создание ноды выпускает ключ сервера и сразу публикует конфигурацию`() {
        val node = nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")

        assertEquals(1, node.version)
        assertTrue(node.core.server.privateKey.startsWith("base64:"))
        assertNotNull(configs.find("node-1"), "конфигурация должна появиться вместе с нодой")
    }

    @Test
    fun `операторские мутации пишут аудит независимо от config event`() {
        nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")
        users.create(UserInput(id = "user-1", name = "Алиса"), "operator")
        nodes.delete("node-1", 1, "admin")

        assertEquals(
            listOf("node.created", "user.created", "node.deleted"),
            auditEvents.map(AuditEvent::action),
        )
        assertEquals(listOf("operator", "operator", "admin"), auditEvents.map(AuditEvent::actor))
    }

    /** Смена ключа обесценила бы все выданные keylink'и, поэтому правка его не трогает. */
    @Test
    fun `правка настроек не меняет ключ сервера`() {
        val created = nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")

        val updated = nodes.update(
            "node-1", 1,
            NodeInput(name = "Переименована", settings = created.core.copy()),
            "operator",
        )

        assertEquals(created.core.server.privateKey, updated.core.server.privateKey)
        assertEquals("Переименована", updated.name)
    }

    @Test
    fun `устаревшая версия отвергается`() {
        nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")

        assertFailsWith<ResourceConflict> { nodes.update("node-1", 99, NodeInput(name = "X"), "operator") }
        assertFailsWith<ResourceConflict> { nodes.delete("node-1", 99, "operator") }
    }

    @Test
    fun `повторное создание ноды отвергается`() {
        nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")

        assertFailsWith<ResourceConflict> { nodes.create(NodeInput(id = "node-1", name = "Вторая"), "operator") }
    }

    /** Прежняя схема удалять ноду не умела вовсе: revoked был единственным способом её погасить. */
    @Test
    fun `удаление ноды уносит борды, гранты и конфигурацию`() {
        nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")
        boards.create(BoardInput(id = "board-1", nodeId = "node-1", name = "Борд", hash = "hash-1"), "operator")
        users.create(UserInput(id = "user-1", name = "Алиса"), "operator")
        users.replaceGrants("user-1", listOf(GrantInput("node-1")), "operator")

        nodes.delete("node-1", 1, "operator")

        assertFailsWith<ResourceNotFound> { nodes.get("node-1") }
        assertNull(boardsRepo.find("node-1", "board-1"))
        assertNull(configs.find("node-1"))
        assertNotNull(usersRepo.find("user-1"), "человек ноду переживает")
        assertTrue(grantsRepo.of("user-1").isEmpty())
    }

    // --- борды ---

    @Test
    fun `борд создаётся только на существующей ноде и публикует её конфигурацию`() {
        assertFailsWith<ResourceNotFound> {
            boards.create(BoardInput(id = "b", nodeId = "missing", name = "Борд", hash = "h"), "operator")
        }

        nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")
        val before = assertNotNull(configs.find("node-1")).revision

        boards.create(BoardInput(id = "board-1", nodeId = "node-1", name = "Борд", hash = "hash-1"), "operator")

        assertEquals(before + 1, assertNotNull(configs.find("node-1")).revision)
    }

    @Test
    fun `удаление борда отзывает гранты на него`() {
        nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")
        boards.create(BoardInput(id = "board-1", nodeId = "node-1", name = "Борд", hash = "hash-1"), "operator")
        users.create(UserInput(id = "user-1", name = "Алиса"), "operator")
        users.replaceGrants("user-1", listOf(GrantInput("node-1", setOf("board-1"))), "operator")

        boards.delete("node-1", "board-1", 1, "operator")

        assertTrue(grantsRepo.of("user-1").isEmpty())
    }

    // --- пользователи ---

    @Test
    fun `хаб выпускает ключ пользователя, если не задан публичный`() {
        val issued = users.create(UserInput(id = "user-1", name = "Алиса"), "operator")
        val external = users.create(
            UserInput(id = "user-2", name = "Боб", publicKey = testKey(5)),
            "operator",
        )

        assertNotNull(issued.privateKey)
        assertNull(external.privateKey)
        assertNotNull(external.publicKey)
    }

    /** Отпечаток уникален во всём флоте, а не в пределах ноды, как было раньше. */
    @Test
    fun `повторный отпечаток отвергается`() {
        users.create(UserInput(id = "user-1", name = "Алиса", publicKey = testKey(5)), "operator")

        assertFailsWith<ResourceConflict> {
            users.create(UserInput(id = "user-2", name = "Клон", publicKey = testKey(5)), "operator")
        }
    }

    /** Правка пользователя обязана пересобрать конфигурации всех нод, где он размещён. */
    @Test
    fun `правка пользователя публикует все его ноды`() {
        listOf("node-1", "node-2").forEach { id ->
            nodes.create(NodeInput(id = id, name = id), "operator")
            boards.create(BoardInput(id = "board-$id", nodeId = id, name = "Борд", hash = "hash-$id"), "operator")
        }
        users.create(UserInput(id = "user-1", name = "Алиса"), "operator")
        users.replaceGrants("user-1", listOf(GrantInput("node-1"), GrantInput("node-2")), "operator")
        val before = listOf("node-1", "node-2").map { assertNotNull(configs.find(it)).revision }

        users.update("user-1", 1, UserInput(name = "Алиса", maxSessions = 5), "operator")

        val after = listOf("node-1", "node-2").map { assertNotNull(configs.find(it)).revision }
        assertEquals(before.map { it + 1 }, after)
    }

    /** Нода, с которой пользователя убрали, тоже должна пересобраться — иначе он на ней останется. */
    @Test
    fun `снятие гранта публикует и покинутую ноду`() {
        listOf("node-1", "node-2").forEach { id ->
            nodes.create(NodeInput(id = id, name = id), "operator")
            boards.create(BoardInput(id = "board-$id", nodeId = id, name = "Борд", hash = "hash-$id"), "operator")
        }
        users.create(UserInput(id = "user-1", name = "Алиса"), "operator")
        users.replaceGrants("user-1", listOf(GrantInput("node-1"), GrantInput("node-2")), "operator")
        val abandoned = assertNotNull(configs.find("node-2")).revision

        users.replaceGrants("user-1", listOf(GrantInput("node-1")), "operator")

        assertEquals(abandoned + 1, assertNotNull(configs.find("node-2")).revision)
        assertTrue(
            assertNotNull(configs.find("node-2")).configToml.decodeToString().contains("[[users]]").not(),
            "на покинутой ноде пользователя быть не должно",
        )
    }

    @Test
    fun `пустой набор бордов означает все включённые борды ноды`() {
        nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")
        boards.create(BoardInput(id = "board-1", nodeId = "node-1", name = "A", hash = "hash-1"), "operator")
        boards.create(
            BoardInput(id = "board-2", nodeId = "node-1", name = "B", hash = "hash-2", state = ResourceState.DISABLED),
            "operator",
        )
        users.create(UserInput(id = "user-1", name = "Алиса"), "operator")

        val granted = users.replaceGrants("user-1", listOf(GrantInput("node-1")), "operator")

        assertEquals(setOf("board-1"), granted.single().boardIds, "выключенный борд в грант не попадает")
    }

    @Test
    fun `грант на чужой борд отвергается`() {
        nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")
        nodes.create(NodeInput(id = "node-2", name = "Вторая"), "operator")
        boards.create(BoardInput(id = "board-1", nodeId = "node-1", name = "A", hash = "hash-1"), "operator")
        boards.create(BoardInput(id = "board-2", nodeId = "node-2", name = "B", hash = "hash-2"), "operator")
        users.create(UserInput(id = "user-1", name = "Алиса"), "operator")

        assertFailsWith<InvalidRequest> {
            users.replaceGrants("user-1", listOf(GrantInput("node-1", setOf("board-2"))), "operator")
        }
    }

    @Test
    fun `ротация ключа меняет отпечаток и публикует ноды`() {
        nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")
        boards.create(BoardInput(id = "board-1", nodeId = "node-1", name = "A", hash = "hash-1"), "operator")
        val created = users.create(UserInput(id = "user-1", name = "Алиса"), "operator")
        users.replaceGrants("user-1", listOf(GrantInput("node-1")), "operator")
        val before = assertNotNull(configs.find("node-1")).revision

        val rotated = users.rotateKey("user-1", 1, "operator")

        assertTrue(rotated.privateKey != created.privateKey)
        assertTrue(rotated.identityFingerprint() != created.identityFingerprint())
        assertEquals(before + 1, assertNotNull(configs.find("node-1")).revision)
    }

    @Test
    fun `ротация невозможна для пользователя с внешним ключом`() {
        users.create(
            UserInput(id = "user-1", name = "Боб", publicKey = testKey(5)),
            "operator",
        )

        assertFailsWith<InvalidRequest> { users.rotateKey("user-1", 1, "operator") }
    }

    @Test
    fun `удаление пользователя публикует ноды, где он был`() {
        nodes.create(NodeInput(id = "node-1", name = "Первая"), "operator")
        boards.create(BoardInput(id = "board-1", nodeId = "node-1", name = "A", hash = "hash-1"), "operator")
        users.create(UserInput(id = "user-1", name = "Алиса"), "operator")
        users.replaceGrants("user-1", listOf(GrantInput("node-1")), "operator")
        val before = assertNotNull(configs.find("node-1")).revision

        users.delete("user-1", 1, "operator")

        assertEquals(before + 1, assertNotNull(configs.find("node-1")).revision)
        assertNull(usersRepo.find("user-1"))
    }

    @Test
    fun `пагинация и фильтры работают`() {
        repeat(3) { index -> nodes.create(NodeInput(id = "node-$index", name = "Нода $index"), "operator") }

        assertEquals(3, nodes.list(null, 0, 10).total)
        assertEquals(2, nodes.list(null, 1, 10).items.size)
        assertEquals(1, nodes.list("node-1", 0, 10).items.size)
    }
}
