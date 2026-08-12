package io.boardproxy.control.provisioning.infrastructure.compiler.toml

import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.provisioning.domain.model.User
import io.boardproxy.control.provisioning.application.CoreConfigCompiler
import java.time.Duration

class TomlCoreConfigCompiler : CoreConfigCompiler {
    override fun compile(catalog: Catalog): ByteArray {
        val assigned = catalog.assignedResources()
        val availableBoards = assigned.boards
            .filter { catalog.node.state.name != "REVOKED" && it.state.name != "REVOKED" }
        val availableBoardIds = availableBoards.map(Board::id).toSet()
        val users = assigned.users.mapNotNull { assignedUser ->
            val user = assignedUser.user
            val boardIds = assignedUser.boardIds.filter(availableBoardIds::contains)
            if (catalog.node.state.name == "REVOKED" || user.state.name == "REVOKED" || boardIds.isEmpty()) null
            else CompiledUser(user, boardIds)
        }

        return buildString {
            appendLine("version = 1")
            appendLine()
            appendLine("[server]")
            property("private_key", catalog.node.core.server.privateKey)
            property("idle_timeout", catalog.node.core.server.idleTimeout.goString())
            property("allow_private_egress", catalog.node.core.server.allowPrivateEgress)
            appendLine()
            appendLine("[transport]")
            catalog.node.core.transport.let {
                property("window", it.window)
                property("max_frame_payload", it.maxFramePayload)
                property("stream_window", it.streamWindow)
                property("max_stream_window", it.maxStreamWindow)
                property("ack_timeout", it.ackTimeout.goString())
                property("coalesce_target", it.coalesceTarget)
                property("stream_idle_timeout", it.streamIdleTimeout.goString())
            }
            appendLine()
            appendLine("[management]")
            property("grpc_listen", catalog.node.core.management.grpcListen)
            catalog.node.core.management.httpListen?.takeIf(String::isNotBlank)?.let { property("http_listen", it) }
            appendLine()
            appendLine("[observability]")
            property("enabled", catalog.node.core.observability.enabled)
            property("log_level", catalog.node.core.observability.logLevel)

            availableBoards.forEach { board ->
                appendLine()
                appendLine("[[boards]]")
                property("tag", board.id)
                property("name", board.name)
                property("hash", board.hash)
                board.hubSlide?.takeIf(String::isNotBlank)?.let { property("hub_slide", it) }
                board.apiBase?.takeIf(String::isNotBlank)?.let { property("api_base", it) }
                board.guestName?.takeIf(String::isNotBlank)?.let { property("guest_name", it) }
                property("enabled", catalog.node.state.isEnabled && board.state.isEnabled)
                property("max_lanes", board.maxLanes)
            }
            users.forEach { compiled ->
                appendLine()
                appendLine("[[users]]")
                property("tag", compiled.user.id)
                property("name", compiled.user.name)
                compiled.user.privateKey?.takeIf(String::isNotBlank)?.let { property("private_key", it) }
                compiled.user.publicKey?.takeIf(String::isNotBlank)?.let { property("public_key", it) }
                property("enabled", catalog.node.state.isEnabled && compiled.user.state.isEnabled)
                property("boards", compiled.boardIds)
                property("max_sessions", compiled.user.maxSessions)
                property("max_lanes", compiled.user.maxLanes)
            }
        }.toByteArray(Charsets.UTF_8)
    }
}

private data class CompiledUser(val user: User, val boardIds: List<String>)

private fun StringBuilder.property(name: String, value: String) = appendLine("  $name = \"${value.tomlEscaped()}\"")
private fun StringBuilder.property(name: String, value: Boolean) = appendLine("  $name = $value")
private fun StringBuilder.property(name: String, value: Int) = appendLine("  $name = $value")
private fun StringBuilder.property(name: String, values: List<String>) =
    appendLine("  $name = ${values.joinToString(prefix = "[", postfix = "]") { "\"${it.tomlEscaped()}\"" }}")

private fun String.tomlEscaped() = buildString {
    for (character in this@tomlEscaped) {
        append(
            when (character) {
                '\\' -> "\\\\"
                '"' -> "\\\""
                '\n' -> "\\n"
                '\r' -> "\\r"
                '\t' -> "\\t"
                else -> character
            },
        )
    }
}

internal fun Duration.goString(): String {
    if (isZero) return "0s"
    require(!isNegative) { "negative durations are not supported" }
    var nanos = toNanos()
    if (nanos < 1_000L) return "${nanos}ns"
    if (nanos < 1_000_000L) return decimalDuration(nanos, 1_000L, "µs")
    if (nanos < 1_000_000_000L) return decimalDuration(nanos, 1_000_000L, "ms")
    val hours = nanos / 3_600_000_000_000L
    nanos %= 3_600_000_000_000L
    val minutes = nanos / 60_000_000_000L
    nanos %= 60_000_000_000L
    val seconds = nanos / 1_000_000_000L
    nanos %= 1_000_000_000L
    return buildString {
        if (hours > 0) append(hours).append('h')
        if (minutes > 0 || hours > 0) append(minutes).append('m')
        append(seconds)
        if (nanos > 0) {
            val fraction = nanos.toString().padStart(9, '0').trimEnd('0')
            append('.').append(fraction)
        }
        append('s')
    }
}

private fun decimalDuration(nanos: Long, unit: Long, suffix: String): String {
    val whole = nanos / unit
    val remainder = nanos % unit
    if (remainder == 0L) return "$whole$suffix"
    val width = unit.toString().length - 1
    val fraction = remainder.toString().padStart(width, '0').trimEnd('0')
    return "$whole.$fraction$suffix"
}
