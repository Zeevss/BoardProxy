package io.boardproxy.control.provisioning.domain.model

/**
 * Пользователь в контексте одной ноды: сам пользователь, его борды на этой ноде
 * и текущее состояние квоты.
 *
 * [quotaExceeded] приходит из телеметрии как обычный вход, а не как команда:
 * превышение влияет на конфигурацию через компилятор, поэтому сброс периода
 * возвращает пользователя в строй сам.
 */
data class UserOnNode(
    val user: User,
    val boardIds: Set<String>,
    val quotaExceeded: Boolean = false,
) {
    /** Состояние, в котором пользователь попадает в конфигурацию ядра. */
    val enabled: Boolean get() = user.state.isEnabled && !quotaExceeded
}

/**
 * Вход компилятора конфигурации — read-model, а не агрегат.
 *
 * Инвариантов здесь нет намеренно: ссылочную целостность держит схема (внешние
 * ключи грантов и бордов), а перевалидация всего набора на каждую правку одного
 * поля была главной причиной, по которой прежний Catalog приходилось заменять
 * целиком.
 */
data class NodeConfigInput(
    val node: Node,
    val boards: List<Board>,
    val users: List<UserOnNode>,
)
