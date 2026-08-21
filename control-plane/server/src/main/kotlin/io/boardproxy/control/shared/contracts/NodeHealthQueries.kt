package io.boardproxy.control.shared.contracts

import java.time.Instant

/**
 * Желаемая ревизия ноды: то, что хаб опубликовал и ждёт увидеть в отчёте.
 *
 * Вместе с `appliedRevision` из отчёта агента даёт расхождение — единственный
 * признак, по которому панель отличает «синхронизируется» от «в сети».
 */
data class DesiredRevision(val revision: Long, val configSha256: String)

/**
 * Порт, а не прямой доступ к репозиторию provisioning: каталог агентов живёт в
 * `shared` и про ограниченные контексты знать не должен.
 */
fun interface DesiredRevisionQueries {
    /** Сразу по всем нодам: список агентов читается одним экраном целиком. */
    fun all(): Map<String, DesiredRevision>
}

/**
 * Свёрнутый runtime-снимок ноды.
 *
 * Панели в списке нужны только итоги; полный снимок с разбивкой по
 * пользователям и бордам читается отдельно, когда оператор открыл карточку.
 */
data class RuntimeTotals(
    val activeSessions: Int,
    val activeLanes: Int,
    val observedAt: Instant,
)

fun interface RuntimeTotalsQueries {
    fun all(): Map<String, RuntimeTotals>
}
