package ru.zevsus.proxy.boardvpn.domain.repository

/**
 * Почему подписку не удалось получить.
 *
 * Раньше любой отказ показывался одинаково — красной рамкой вокруг поля без
 * единого слова. Отличить обрезанную ссылку от лежащего сервиса было нельзя, а
 * лечатся они по-разному: первое — повторным копированием, второе — ожиданием.
 */
enum class SubscriptionFailureReason {
    /** Ссылка неполная: чаще всего у неё отрезан хвост `#bp1=…`. */
    LINK,

    /** Сервис ответил и отказал: подписки нет, она отозвана или выключена. */
    REJECTED,

    /** Подписка жива, но включённых ключей в ней нет — подключаться не к чему. */
    EMPTY,

    /** Не ответили ни основной канал, ни резервный. Единственный случай, который стоит повторить. */
    UNREACHABLE,

    /** Всё остальное: показываем общий текст, не притворяясь, что знаем причину. */
    UNKNOWN,
}

class SubscriptionFailure(
    val reason: SubscriptionFailureReason,
    cause: Throwable? = null,
) : Exception(reason.name, cause) {

    companion object {
        /**
         * Код причины едет в начале сообщения от Go: `bp-reason:unreachable; …`.
         * gomobile переносит через границу только текст, типов там не остаётся.
         */
        private const val REASON_PREFIX = "bp-reason:"

        fun fromBridge(cause: Throwable): SubscriptionFailure {
            val message = cause.message.orEmpty()
            val code = message.substringAfter(REASON_PREFIX, "").substringBefore(';').trim()
            val reason = when (code) {
                "link" -> SubscriptionFailureReason.LINK
                "rejected" -> SubscriptionFailureReason.REJECTED
                "empty" -> SubscriptionFailureReason.EMPTY
                "unreachable" -> SubscriptionFailureReason.UNREACHABLE
                else -> SubscriptionFailureReason.UNKNOWN
            }
            return SubscriptionFailure(reason, cause)
        }
    }
}
