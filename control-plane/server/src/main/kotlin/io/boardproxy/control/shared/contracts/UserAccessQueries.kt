package io.boardproxy.control.shared.contracts

/** Доступ пользователя на одной ноде. [keylink] == null — рабочей ссылки сейчас нет. */
data class NodeKeylink(
    val nodeId: String,
    val nodeName: String,
    val keylink: String?,
)

/**
 * Ключи пользователя по всем нодам его размещения.
 *
 * Подписка больше не хранит список пар «нода — пользователь»: он выводится из
 * грантов, поэтому разойтись с размещением не может. Порт живёт в shared,
 * чтобы подписки и provisioning не ссылались друг на друга.
 */
fun interface KeylinkQueries {
    fun forUser(userId: String, label: String): List<NodeKeylink>
}

/** Расход пользователя: сумма по флоту и разбивка по нодам. */
data class UserUsage(
    val limitBytes: Long,
    val usedBytes: Long,
    val perNode: Map<String, Long>,
)

fun interface UserUsageQueries {
    fun usage(userId: String): UserUsage
}
