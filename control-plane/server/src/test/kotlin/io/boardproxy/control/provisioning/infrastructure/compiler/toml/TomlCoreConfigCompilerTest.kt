package io.boardproxy.control.provisioning.infrastructure.compiler.toml

import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.CoreSettings
import io.boardproxy.control.provisioning.domain.model.Node
import io.boardproxy.control.provisioning.domain.model.NodeConfigInput
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.User
import io.boardproxy.control.provisioning.domain.model.UserOnNode
import java.nio.file.Path
import java.time.Duration
import java.time.Instant
import java.util.Base64
import kotlin.test.Test
import kotlin.test.assertContains
import kotlin.test.assertEquals
import kotlin.io.path.exists
import kotlin.io.path.readText
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class TomlCoreConfigCompilerTest {
    private val compiler = TomlCoreConfigCompiler()

    /**
     * Золотой TOML унаследован от прежнего компилятора без единого изменения:
     * смена модели не должна была поменять ни байта в том, что читает ядро.
     */
    @Test
    fun `конфигурация побайтово совпадает с прежним компилятором`() {
        assertEquals(EXPECTED_TOML, compiler.compile(input()).decodeToString())
    }

    /**
     * Вторая половина защиты от риска R3: формат TOML реализован дважды —
     * здесь на Kotlin и в ядре на Go. Обе стороны привязаны к одной фикстуре,
     * и парсер ядра читает её в `core/internal/serverconfig/hubconfig_test.go`.
     * Расхождение уронит один из двух тестов, а не прод.
     */
    @Test
    fun `вывод совпадает с фикстурой, которую читает ядро`() {
        val fixture = Path.of("../contracts/testdata/hub-config.toml")

        assertTrue(fixture.exists(), "фикстура совместимости с ядром пропала: $fixture")
        assertEquals(fixture.readText(), compiler.compile(input()).decodeToString())
    }

    @Test
    fun `порядок строк на входе не влияет на результат`() {
        val source = input()
        val reordered = source.copy(
            boards = source.boards.reversed(),
            users = source.users.reversed(),
        )

        assertEquals(compiler.compile(source).decodeToString(), compiler.compile(reordered).decodeToString())
    }

    @Test
    fun `секрет отозванного пользователя не попадает в конфигурацию`() {
        val source = input()
        val zoe = source.users.first { it.user.id == "zoe" }
        val revoked = source.copy(
            users = source.users.map {
                if (it.user.id == "zoe") it.copy(user = it.user.copy(state = ResourceState.REVOKED)) else it
            },
        )

        val config = compiler.compile(revoked).decodeToString()

        assertFalse(config.contains("tag = \"zoe\""))
        assertFalse(config.contains(zoe.user.privateKey!!))
    }

    /**
     * Квота гасит пользователя тем же флагом, что и ручное выключение: он
     * остаётся в конфигурации, поэтому снятие превышения возвращает его в строй
     * без вмешательства оператора.
     */
    @Test
    fun `исчерпанная квота выключает пользователя, но не удаляет его`() {
        val exhausted = input().let { source ->
            source.copy(users = source.users.map { if (it.user.id == "zoe") it.copy(quotaExceeded = true) else it })
        }

        val config = compiler.compile(exhausted).decodeToString()

        assertContains(config, "tag = \"zoe\"")
        assertEquals(1, Regex("""tag = "zoe"\n(?:.*\n)*?  enabled = false""").findAll(config).count())
    }

    @Test
    fun `пользователь без доступных бордов в конфигурацию не попадает`() {
        val orphaned = input().let { source ->
            source.copy(users = source.users.map { if (it.user.id == "zoe") it.copy(boardIds = setOf("missing")) else it })
        }

        assertFalse(compiler.compile(orphaned).decodeToString().contains("tag = \"zoe\""))
    }

    @Test
    fun `отозванная нода не выдаёт ни бордов, ни пользователей`() {
        val revoked = input().let { it.copy(node = it.node.copy(state = ResourceState.REVOKED)) }

        val config = compiler.compile(revoked).decodeToString()

        assertFalse(config.contains("[[boards]]"))
        assertFalse(config.contains("[[users]]"))
    }

    @Test
    fun `борд чужой ноды отбрасывается`() {
        val foreign = input().let { source ->
            source.copy(boards = source.boards + source.boards.first().copy(nodeId = "node-2", id = "foreign"))
        }

        assertFalse(compiler.compile(foreign).decodeToString().contains("tag = \"foreign\""))
    }

    @Test
    fun `длительности используют формат Go`() {
        assertEquals("500ms", Duration.ofMillis(500).goString())
        assertEquals("1.5s", Duration.ofMillis(1_500).goString())
        assertEquals("1h0m0s", Duration.ofHours(1).goString())
        assertEquals("250µs", Duration.ofNanos(250_000).goString())
    }

    private fun input(): NodeConfigInput {
        val now = Instant.ofEpochSecond(100)
        return NodeConfigInput(
            node = Node("node-1", "Node 1", ResourceState.ENABLED, CoreSettings.defaults(key(1)), 1, now),
            boards = listOf(
                Board("node-1", "zeta", "Zeta", "hash-z", state = ResourceState.ENABLED, maxLanes = 3, version = 1, updatedAt = now),
                Board("node-1", "alpha", "Alpha", "hash-a", apiBase = "https://example.test/api", state = ResourceState.ENABLED, maxLanes = 2, version = 1, updatedAt = now),
            ),
            users = listOf(
                UserOnNode(
                    User("zoe", "Zoe", privateKey = key(4), state = ResourceState.ENABLED, maxSessions = 0, maxLanes = 2, version = 1, updatedAt = now),
                    boardIds = setOf("zeta"),
                ),
                UserOnNode(
                    User("alice", "Alice", privateKey = key(3), state = ResourceState.ENABLED, maxSessions = 2, maxLanes = 2, version = 1, updatedAt = now),
                    boardIds = setOf("zeta", "alpha"),
                ),
            ),
        )
    }
}

private fun key(value: Byte): String = "base64:" + Base64.getEncoder().encodeToString(ByteArray(32) { value })

private val EXPECTED_TOML = """
version = 1

[server]
  private_key = "base64:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
  idle_timeout = "1m30s"
  allow_private_egress = false

[transport]
  window = 0
  max_frame_payload = 4194304
  stream_window = 1048576
  max_stream_window = 33554432
  ack_timeout = "6s"
  coalesce_target = 0
  stream_idle_timeout = "0s"

[management]
  grpc_listen = "unix:///run/bproxy/control.sock"

[observability]
  enabled = true
  log_level = "info"

[[boards]]
  tag = "alpha"
  name = "Alpha"
  hash = "hash-a"
  api_base = "https://example.test/api"
  enabled = true
  max_lanes = 2

[[boards]]
  tag = "zeta"
  name = "Zeta"
  hash = "hash-z"
  enabled = true
  max_lanes = 3

[[users]]
  tag = "alice"
  name = "Alice"
  private_key = "base64:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM="
  enabled = true
  boards = ["alpha", "zeta"]
  max_sessions = 2
  max_lanes = 2

[[users]]
  tag = "zoe"
  name = "Zoe"
  private_key = "base64:BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ="
  enabled = true
  boards = ["zeta"]
  max_sessions = 0
  max_lanes = 2
""".trimStart()
