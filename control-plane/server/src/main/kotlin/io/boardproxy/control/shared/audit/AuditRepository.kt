package io.boardproxy.control.shared.audit

fun interface AuditRepository {
    fun append(event: AuditEvent)
}

/**
 * Страница журнала. Своя, а не общая с provisioning: `shared` не должен знать
 * про ограниченные контексты, а ради одного типа тянуть его сюда незачем.
 */
data class AuditPage(
    val items: List<AuditEvent>,
    val offset: Int,
    val limit: Int,
    val total: Long,
)

/**
 * Чтение журнала для ленты активности в панели.
 *
 * Живая лента приходит по SSE, но поток только транслирует и ничего не хранит:
 * очередь ограничена, а после переподключения панель не знает, что пропустила.
 * История нужна ровно для того, чтобы экран не был пустым до первого события.
 */
interface AuditQueries {
    fun list(nodeId: String?, offset: Int, limit: Int): AuditPage
}
