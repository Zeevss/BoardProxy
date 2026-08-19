package io.boardproxy.control.provisioning.domain.model

/** Пользователь и его борды на конкретной ноде — развёрнутый [Grant]. */
data class UserPlacement(val user: User, val boardIds: Set<String>)

/**
 * Владеемое состояние ноды: только то, что задал оператор.
 *
 * Именно это уходит в снимок истории — не скомпилированный TOML. Rollback
 * применяет снимок как обычные записи и пересобирает конфигурацию заново,
 * поэтому хранить производную форму незачем.
 *
 * Наблюдаемого здесь нет: состояние квот приходит отдельно и превращает
 * [NodeState] в [NodeConfigInput] через [withQuotas].
 */
data class NodeState(
    val node: Node,
    val boards: List<Board>,
    val placements: List<UserPlacement>,
) {
    fun withQuotas(exceededUsers: Set<String>): NodeConfigInput = NodeConfigInput(
        node = node,
        boards = boards,
        users = placements.map { placement ->
            UserOnNode(
                user = placement.user,
                boardIds = placement.boardIds,
                quotaExceeded = placement.user.id in exceededUsers,
            )
        },
    )
}
