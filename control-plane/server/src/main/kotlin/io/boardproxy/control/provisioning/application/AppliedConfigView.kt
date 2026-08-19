package io.boardproxy.control.provisioning.application

import java.time.Instant

data class AppliedConfig(
    val nodeId: String,
    val revision: Long,
    /** SHA-256 полного скомпилированного TOML — того, что реально применяет нода. */
    val configSha256: String,
    val toml: String,
    val updatedAt: Instant,
)

fun interface AppliedConfigQueries {
    fun latest(nodeId: String): AppliedConfig?
}

/** Текущая конфигурация ноды для панели — всегда после вырезания секретов. */
class AppliedConfigService(private val configs: DesiredConfigRepository) : AppliedConfigQueries {
    override fun latest(nodeId: String): AppliedConfig? {
        val config = configs.find(nodeId) ?: return null
        return AppliedConfig(
            nodeId = config.nodeId,
            revision = config.revision,
            configSha256 = config.configSha256,
            toml = CoreConfigRedaction.redact(config.configToml.toString(Charsets.UTF_8)),
            updatedAt = config.updatedAt,
        )
    }
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
