package io.boardproxy.control.delivery.application

import io.boardproxy.control.delivery.domain.NodeStatus
import kotlinx.coroutines.flow.Flow
import java.time.Duration
import java.time.Instant

interface NodeStatusRepository {
    fun find(nodeId: String): NodeStatus?
    fun save(status: NodeStatus, expectedVersion: Long): Boolean
}

interface DesiredRevisionSignals {
    fun changes(nodeId: String): Flow<Unit>
}

fun interface NodeStatusNotifier {
    fun changed(status: NodeStatus)
}

data class NodeSessionLease(
    val nodeId: String,
    val ownerId: String,
    val sessionId: String,
    val fencingToken: Long,
    val expiresAt: Instant,
)

interface NodeSessionLeaseRepository {
    fun acquire(
        nodeId: String,
        ownerId: String,
        sessionId: String,
        now: Instant,
        ttl: Duration,
    ): NodeSessionLease?
    fun renew(lease: NodeSessionLease, now: Instant, ttl: Duration): NodeSessionLease?
    fun release(lease: NodeSessionLease)
    fun expireStatuses(now: Instant): Int
}
