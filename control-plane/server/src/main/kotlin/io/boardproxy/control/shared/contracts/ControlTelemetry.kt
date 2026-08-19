package io.boardproxy.control.shared.contracts

/**
 * Счётчики, которые ведут прикладные сервисы.
 *
 * Порт, а не MeterRegistry напрямую: прикладной слой не должен знать про
 * систему метрик, а тесты не должны её поднимать. Реализация по умолчанию
 * ничего не делает.
 */
interface ControlTelemetry {
    /** changed = false означает, что правка не изменила ни байта конфигурации. */
    fun configPublished(changed: Boolean) = Unit

    /** fresh = false означает повторный отчёт, отсечённый по batch_id. */
    fun reportAccepted(fresh: Boolean) = Unit

    companion object {
        val NOOP: ControlTelemetry = object : ControlTelemetry {}
    }
}
