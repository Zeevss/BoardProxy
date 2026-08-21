package io.boardproxy.control.shared.contracts

import java.time.Instant

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

/**
 * Состояние квоты одного пользователя в текущем периоде.
 *
 * `null` в карте [UserQuotaSummaryQueries.all] означает «квоты нет», а не
 * «квота нулевая»: лимит в базе всегда положительный, поэтому безлимитного
 * пользователя выражает именно отсутствие записи или снятый [enabled].
 */
data class UserQuotaSummary(
    val limitBytes: Long,
    val usedBytes: Long,
    val exceeded: Boolean,
    val enabled: Boolean,
    val periodEnd: Instant,
)

/**
 * Квоты всего флота разом — для списка пользователей.
 *
 * Поштучное чтение здесь неприемлемо: строк в списке полсотни, и каждая тянула
 * бы отдельный запрос к панели ради одного процента заполнения.
 */
fun interface UserQuotaSummaryQueries {
    fun all(): Map<String, UserQuotaSummary>
}

/**
 * Когда ядро в последний раз отнесло трафик на пользователя.
 *
 * Отсюда же выводится «активирован»: отдельной колонки нет, потому что первый
 * же байт трафика и есть факт активации, а хранить его дублем — заводить
 * состояние, которое умеет разойтись с наблюдаемым.
 */
fun interface UserActivityQueries {
    fun lastSeen(): Map<String, Instant>
}
