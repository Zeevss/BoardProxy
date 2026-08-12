package io.boardproxy.control.delivery.application

import io.boardproxy.control.delivery.domain.AppliedState
import io.boardproxy.control.delivery.domain.ApplyOutcome
import io.boardproxy.control.delivery.domain.Heartbeat
import io.boardproxy.control.delivery.domain.NodeHello
import io.boardproxy.control.delivery.domain.NodeStatus
import io.boardproxy.control.provisioning.application.ConfigRevisionRepository
import io.boardproxy.control.provisioning.domain.model.ConfigRevision
import io.boardproxy.control.shared.errors.ResourceConflict
import java.time.Duration
import java.time.Instant

class NodeSession(
    private val nodeId: String,
    private val revisions: ConfigRevisionRepository,
    private val statuses: NodeStatusRepository,
    private val notifier: NodeStatusNotifier,
    applied: AppliedState,
    private val fencingToken: Long = 0,
) {
    private var bootId = ""
    private var applied = applied
    private var lastSent = AppliedState()
    private var retryAfter = Instant.EPOCH

    @Synchronized
    fun connected(hello: NodeHello, now: Instant) {
        bootId = hello.bootId
        applied = AppliedState(hello.appliedRevision, hello.configSha256)
        updateStatus {
            it.copy(
                connected = true, bootId = hello.bootId, agentVersion = hello.agentVersion,
                coreVersion = hello.coreVersion, appliedRevision = hello.appliedRevision,
                configSha256 = hello.configSha256.ifBlank { null }, lastSeen = now,
                fencingToken = fencingToken,
            )
        }
    }

    @Synchronized
    fun pendingDesired(now: Instant): ConfigRevision? {
        if (now < retryAfter) return null
        val desired = revisions.latest(nodeId) ?: return null
        updateStatus { status ->
            if (status.desiredRevision == desired.revision) status else status.copy(desiredRevision = desired.revision)
        }
        val target = AppliedState(desired.revision, desired.configSha256)
        if (desired.revision < applied.revision || target == applied || target == lastSent) return null
        lastSent = target
        return desired
    }

    @Synchronized
    fun recordApply(outcome: ApplyOutcome, now: Instant) {
        if (outcome.error.isBlank()) {
            applied = AppliedState(outcome.desiredRevision, outcome.configSha256)
            retryAfter = Instant.EPOCH
        } else {
            lastSent = AppliedState()
            retryAfter = now.plus(RETRY_DELAY)
        }
        updateStatus { status ->
            status.copy(
                desiredRevision = maxOf(status.desiredRevision, outcome.desiredRevision),
                appliedRevision = if (outcome.error.isBlank()) outcome.desiredRevision else status.appliedRevision,
                configSha256 = if (outcome.error.isBlank()) outcome.configSha256 else status.configSha256,
                lastError = outcome.error.ifBlank { null }, lastSeen = now, lastApply = outcome,
            )
        }
    }

    @Synchronized
    fun recordHeartbeat(heartbeat: Heartbeat) {
        if (heartbeat.appliedRevision != applied.revision) {
            applied = AppliedState(heartbeat.appliedRevision)
            lastSent = AppliedState()
            retryAfter = Instant.EPOCH
        }
        updateStatus {
            it.copy(
                coreRunning = heartbeat.coreRunning, coreReady = heartbeat.coreReady,
                appliedRevision = heartbeat.appliedRevision, lastError = heartbeat.error.ifBlank { null },
                lastSeen = heartbeat.sampledAt,
            )
        }
    }

    @Synchronized
    fun disconnected(now: Instant) {
        updateStatus { status ->
            if (status.bootId != bootId) status
            else status.copy(connected = false, coreReady = false, lastSeen = now)
        }
    }

    private fun updateStatus(change: (NodeStatus) -> NodeStatus) {
        repeat(4) {
            val current = statuses.find(nodeId) ?: NodeStatus(nodeId)
            if (current.fencingToken > fencingToken) return
            val changed = change(current)
            if (changed == current) return
            val saved = changed.copy(version = current.version + 1)
            if (statuses.save(saved, current.version)) {
                notifier.changed(saved)
                return
            }
        }
        throw ResourceConflict("node $nodeId status changed concurrently")
    }

    private companion object {
        val RETRY_DELAY: Duration = Duration.ofSeconds(30)
    }
}
