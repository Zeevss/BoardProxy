package io.boardproxy.control.shared.contracts

/** Секрет показывается ровно один раз — хранится только его хеш. */
data class IssuedServiceToken(val id: String, val secret: String)

/**
 * Выпуск учётных данных для внешнего сервиса.
 *
 * Подписки должны уметь выдать сервису токен, но не должны знать про роли,
 * сроки и устройство хранилища доступа — поэтому здесь узкий порт, а не прямая
 * зависимость на access.
 */
interface ServiceTokenIssuer {
    fun issueSubscriberToken(name: String, actor: String): IssuedServiceToken
    fun revoke(tokenId: String, actor: String)
}
