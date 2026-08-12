package io.boardproxy.control.provisioning.infrastructure.compiler.toml

import io.boardproxy.control.provisioning.domain.model.AssignedUser
import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.provisioning.domain.model.CoreSettings
import io.boardproxy.control.provisioning.domain.model.Node
import io.boardproxy.control.provisioning.domain.model.NodeAssignment
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.User
import java.time.Duration
import java.time.Instant
import java.util.Base64
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse

class TomlCoreConfigCompilerTest {
    @Test
    fun `compiler is deterministic and compatible with Go core boundary`() {
        val catalog = catalog()
        val reordered = catalog.copy(
            boards = catalog.boards.reversed(),
            users = catalog.users.reversed(),
            assignment = catalog.assignment.copy(
                boardIds = catalog.assignment.boardIds.reversed(),
                users = catalog.assignment.users.reversed(),
            ),
        )

        val compiler = TomlCoreConfigCompiler()
        assertEquals(EXPECTED_TOML, compiler.compile(catalog).decodeToString())
        assertEquals(EXPECTED_TOML, compiler.compile(reordered).decodeToString())
    }

    @Test
    fun `revoked user secret never reaches config`() {
        val source = catalog()
        val revoked = source.copy(users = source.users.map { if (it.id == "zoe") it.copy(state = ResourceState.REVOKED) else it })
        val config = TomlCoreConfigCompiler().compile(revoked).decodeToString()

        assertFalse(config.contains("tag = \"zoe\""))
        assertFalse(config.contains(source.users.first { it.id == "zoe" }.privateKey!!))
    }

    @Test
    fun `durations use the exact Go config representation`() {
        assertEquals("500ms", Duration.ofMillis(500).goString())
        assertEquals("1.5s", Duration.ofMillis(1_500).goString())
        assertEquals("1h0m0s", Duration.ofHours(1).goString())
        assertEquals("250µs", Duration.ofNanos(250_000).goString())
    }

    private fun catalog(): Catalog {
        val now = Instant.ofEpochSecond(100)
        return Catalog(
            node = Node("node-1", "Node 1", ResourceState.ENABLED, CoreSettings.defaults(key(1)), 1, now),
            boards = listOf(
                Board("zeta", "Zeta", "hash-z", state = ResourceState.ENABLED, maxLanes = 3, version = 1, updatedAt = now),
                Board("alpha", "Alpha", "hash-a", apiBase = "https://example.test/api", state = ResourceState.ENABLED, maxLanes = 2, version = 1, updatedAt = now),
            ),
            users = listOf(
                User("zoe", "Zoe", privateKey = key(4), state = ResourceState.ENABLED, maxSessions = 0, maxLanes = 2, version = 1, updatedAt = now),
                User("alice", "Alice", privateKey = key(3), state = ResourceState.ENABLED, maxSessions = 2, maxLanes = 2, version = 1, updatedAt = now),
            ),
            assignment = NodeAssignment(
                "node-1", listOf("zeta", "alpha"),
                listOf(AssignedUser("zoe", listOf("zeta")), AssignedUser("alice", listOf("zeta", "alpha"))),
                1, now,
            ),
            version = 1,
            updatedAt = now,
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
