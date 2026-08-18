package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.ResourceState
import java.time.Instant

/**
 * Борд вместе с нодой, на которой он работает. Панель показывает весь флот
 * сразу, поэтому выборка идёт по всем нодам, а не по выбранной.
 */
data class FleetBoard(
    val nodeId: String,
    val nodeName: String,
    val nodeState: ResourceState,
    val id: String,
    val name: String,
    val hash: String,
    // Необязательные поля борда переносятся в ответ целиком: панель шлёт борд
    // обратно полным телом, и без них они были бы затёрты.
    val hubSlide: String?,
    val apiBase: String?,
    val guestName: String?,
    val state: ResourceState,
    val maxLanes: Int,
    val assigned: Boolean,
    val users: Int,
    val version: Long,
    val updatedAt: Instant,
)

fun interface FleetBoardQueries {
    fun list(query: String?): List<FleetBoard>
}
