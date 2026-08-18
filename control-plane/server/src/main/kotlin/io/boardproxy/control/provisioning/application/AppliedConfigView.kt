package io.boardproxy.control.provisioning.application

import java.time.Instant

data class AppliedConfig(
    val nodeId: String,
    val revision: Long,
    val catalogVersion: Long,
    /** SHA-256 полного скомпилированного TOML — того, что реально применяет нода. */
    val configSha256: String,
    val toml: String,
    val createdAt: Instant,
)

fun interface AppliedConfigQueries {
    fun latest(nodeId: String): AppliedConfig?
}

/**
 * Скомпилированный TOML содержит приватный ключ ноды и приватные ключи всех
 * пользователей, поэтому в панель он уходит только после вырезания секретов:
 * остаётся конфигурация ноды, а идентичности клиентов не показываются вовсе.
 */
object CoreConfigRedaction {
    private const val REDACTED = "  # private_key: скрыт панелью"

    fun redact(toml: String): String {
        val result = StringBuilder()
        var insideUsers = false
        toml.lineSequence().forEach { line ->
            val trimmed = line.trimStart()
            if (trimmed.startsWith("[")) insideUsers = trimmed.startsWith("[[users]]")
            if (insideUsers) return@forEach
            if (isPrivateKey(trimmed)) {
                result.appendLine(REDACTED)
                return@forEach
            }
            result.appendLine(line)
        }
        return result.toString().trimEnd() + "\n"
    }

    /** Ключ может быть записан как private_key или "private_key" — оба варианта секретны. */
    private fun isPrivateKey(trimmed: String): Boolean {
        val key = trimmed.substringBefore('=', missingDelimiterValue = "").trim().trim('"')
        return key == "private_key"
    }
}
