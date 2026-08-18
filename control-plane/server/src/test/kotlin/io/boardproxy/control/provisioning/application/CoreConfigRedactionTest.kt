package io.boardproxy.control.provisioning.application

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class CoreConfigRedactionTest {
    private val compiled = """
        version = 1

        [server]
          private_key = "base64:SUPER-SECRET-NODE-KEY"
          idle_timeout = "1m30s"
          allow_private_egress = false

        [management]
          grpc_listen = "unix:///run/bproxy/control.sock"

        [[boards]]
          id = "primary"
          name = "Primary"
          max_lanes = 4

        [[users]]
          id = "alice"
          private_key = "base64:ALICE-SECRET"
          max_sessions = 2

        [[users]]
          id = "bob"
          private_key = "base64:BOB-SECRET"
          max_sessions = 1
    """.trimIndent()

    @Test
    fun `no private key survives redaction`() {
        val redacted = CoreConfigRedaction.redact(compiled)

        listOf("SUPER-SECRET-NODE-KEY", "ALICE-SECRET", "BOB-SECRET").forEach {
            assertFalse(redacted.contains(it), "секрет остался в выдаче: $it")
        }
    }

    @Test
    fun `user identities are not shown at all`() {
        val redacted = CoreConfigRedaction.redact(compiled)

        assertFalse(redacted.contains("[[users]]"))
        assertFalse(redacted.contains("alice"))
        assertFalse(redacted.contains("bob"))
    }

    @Test
    fun `node configuration survives intact`() {
        val redacted = CoreConfigRedaction.redact(compiled)

        listOf(
            "version = 1", "[server]", "idle_timeout", "allow_private_egress",
            "[management]", "grpc_listen", "[[boards]]", "primary", "max_lanes = 4",
        ).forEach { assertTrue(redacted.contains(it), "потеряна строка ноды: $it") }
    }

    @Test
    fun `a section after users is not swallowed by redaction`() {
        val withTrailingSection = "$compiled\n\n[observability]\n  log_level = \"info\"\n"

        val redacted = CoreConfigRedaction.redact(withTrailingSection)

        // Вырезание [[users]] обязано закончиться на следующем заголовке, а не съесть остаток файла.
        assertTrue(redacted.contains("[observability]"))
        assertTrue(redacted.contains("log_level"))
        assertFalse(redacted.contains("ALICE-SECRET"))
    }

    @Test
    fun `a quoted private key is redacted too`() {
        val quoted = "[server]\n  \"private_key\" = \"base64:QUOTED\"\n"

        assertFalse(CoreConfigRedaction.redact(quoted).contains("QUOTED"))
    }

    @Test
    fun `redaction leaves a visible marker instead of silently dropping the line`() {
        val redacted = CoreConfigRedaction.redact("[server]\n  private_key = \"x\"\n")

        assertEquals("[server]\n  # private_key: скрыт панелью", redacted.trimEnd())
    }
}
