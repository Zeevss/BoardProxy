package io.boardproxy.control.provisioning.infrastructure.compiler.toml

import io.boardproxy.control.provisioning.application.CoreConfigCompiler
import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.NodeConfigInput
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.UserOnNode
import java.time.Duration

/**
 * Компилятор конфигурации ядра.
 *
 * Чистая функция: одинаковый вход всегда даёт одинаковые байты. На этом держится
 * публикация — ревизия появляется только когда sha256 результата изменилась,
 * поэтому любая недетерминированность здесь превратилась бы в поток ложных
 * ревизий и лишних перезапусков ядра. Отсюда явные сортировки, а не порядок,
 * в котором строки пришли из базы.
 */
class TomlCoreConfigCompiler : CoreConfigCompiler {
    override fun compile(input: NodeConfigInput): ByteArray {
        val nodeRevoked = input.node.state == ResourceState.REVOKED

        val boards = if (nodeRevoked) {
            emptyList()
        } else {
            input.boards
                .filter { it.nodeId == input.node.id && it.state != ResourceState.REVOKED }
                .sortedBy(Board::id)
        }
        val boardIds = boards.map(Board::id).toSet()

        val users = if (nodeRevoked) {
            emptyList()
        } else {
            input.users
                .filter { it.user.state != ResourceState.REVOKED }
                .map { placement -> placement to placement.boardIds.filter(boardIds::contains).sorted() }
                .filter { (_, granted) -> granted.isNotEmpty() }
                .sortedBy { (placement, _) -> placement.user.id }
        }

        return buildString {
            appendLine("version = 1")
            appendLine()
            appendLine("[server]")
            property("private_key", input.node.core.server.privateKey)
            property("idle_timeout", input.node.core.server.idleTimeout.goString())
            property("allow_private_egress", input.node.core.server.allowPrivateEgress)
            appendLine()
            appendLine("[transport]")
            input.node.core.transport.let {
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
            property("grpc_listen", input.node.core.management.grpcListen)
            input.node.core.management.httpListen?.takeIf(String::isNotBlank)?.let { property("http_listen", it) }
            appendLine()
            appendLine("[observability]")
            property("enabled", input.node.core.observability.enabled)
            property("log_level", input.node.core.observability.logLevel)

            boards.forEach { board ->
                appendLine()
                appendLine("[[boards]]")
                property("tag", board.id)
                property("name", board.name)
                property("hash", board.hash)
                board.hubSlide?.takeIf(String::isNotBlank)?.let { property("hub_slide", it) }
                board.apiBase?.takeIf(String::isNotBlank)?.let { property("api_base", it) }
                board.guestName?.takeIf(String::isNotBlank)?.let { property("guest_name", it) }
                property("enabled", input.node.state.isEnabled && board.state.isEnabled)
                property("max_lanes", board.maxLanes)
            }
            users.forEach { (placement, granted) ->
                appendLine()
                appendLine("[[users]]")
                property("tag", placement.user.id)
                property("name", placement.user.name)
                placement.user.privateKey?.takeIf(String::isNotBlank)?.let { property("private_key", it) }
                placement.user.publicKey?.takeIf(String::isNotBlank)?.let { property("public_key", it) }
                // Исчерпанная квота гасит пользователя ровно тем же флагом, что и
                // ручное выключение: отдельного механизма принуждения нет.
                property("enabled", input.node.state.isEnabled && placement.enabled)
                property("boards", granted)
                property("max_sessions", placement.user.maxSessions)
                property("max_lanes", placement.user.maxLanes)
            }
        }.toByteArray(Charsets.UTF_8)
    }
}

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

/**
 * Повторяет формат time.Duration.String() из Go: конфигурацию читает ядро на Go,
 * и любое расхождение здесь всплывёт только в проде.
 */
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
