package io.boardproxy.control.runtime.application

import java.time.Instant

/**
 * Последний снимок, присланный нодой, как есть.
 *
 * Проекции по событиям больше нет: нода знает своё состояние лучше, чем хаб мог
 * бы восстановить его из журнала. Вместе с проекцией исчезли детекция разрывов,
 * авторитетная замена снимком, реплей и ручное перестроение.
 */
data class RuntimeSnapshotView(
    val nodeId: String,
    val snapshot: Map<String, Any?>,
    val observedAt: Instant,
)

/** Запись журнала активности. Ничего не проецирует, поэтому разрыв безвреден. */
data class RuntimeEventView(
    val id: Long,
    val type: String,
    val payload: Map<String, Any?>,
    val occurredAt: Instant,
    val receivedAt: Instant,
)

interface RuntimeQueries {
    fun snapshot(nodeId: String): RuntimeSnapshotView?
    fun events(nodeId: String, offset: Int, limit: Int): List<RuntimeEventView>
    fun countEvents(nodeId: String): Long
}
