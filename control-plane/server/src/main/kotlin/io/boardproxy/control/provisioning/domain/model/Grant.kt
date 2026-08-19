package io.boardproxy.control.provisioning.domain.model

/**
 * Размещение пользователя: на какой ноде и на каких её бордах он имеет доступ.
 *
 * Заменяет собой связки node_users, node_user_boards и assignment_versions.
 * Ссылочную целостность держит внешний ключ на boards(node_id, id), поэтому
 * здесь проверяется только форма идентификаторов.
 */
data class Grant(
    val userId: String,
    val nodeId: String,
    val boardIds: Set<String>,
) {
    init {
        requireDomain(validId(userId) && validId(nodeId), "invalid grant identity")
        requireDomain(boardIds.isNotEmpty(), "grant must reference at least one board")
        requireDomain(boardIds.all(::validId), "invalid grant board reference")
    }
}
