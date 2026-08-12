package io.boardproxy.control.runtime.application

import io.boardproxy.control.runtime.domain.RuntimeEvent
import io.boardproxy.control.runtime.domain.RuntimeProjection
import io.boardproxy.control.runtime.domain.RuntimeSnapshot
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.shared.errors.ResourceConflict

data class RuntimeEventBatch(
    val nodeId: String,
    val batchId: String,
    val events: List<RuntimeEvent>,
    val snapshot: RuntimeSnapshot?,
    val rawPayload: ByteArray,
)

data class RuntimeIngestionResult(
    val accepted: Boolean,
    val projectionChanged: Boolean,
)

fun interface RuntimeEventIngestion {
    fun store(batch: RuntimeEventBatch): RuntimeIngestionResult
}

interface RuntimeEventStore {
    fun claimBatch(batch: RuntimeEventBatch): Boolean
    fun appendEvent(nodeId: String, event: RuntimeEvent): Boolean
    fun lockProjection(nodeId: String): RuntimeProjection
    fun saveProjection(projection: RuntimeProjection)
}

data class RuntimeReplayMaterial(
    val snapshot: RuntimeSnapshot,
    val events: List<RuntimeEvent>,
)

fun interface RuntimeReplayStore {
    fun material(nodeId: String): RuntimeReplayMaterial?
}

fun interface RuntimeProjectionRebuild {
    fun rebuild(nodeId: String): RuntimeProjection
}

fun interface RuntimeProjectionNotifier {
    fun changed(projection: RuntimeProjection)
}

class RuntimeEventService(
    private val store: RuntimeEventStore,
    private val transactions: TransactionRunner,
    private val notifier: RuntimeProjectionNotifier,
) : RuntimeEventIngestion {
    override fun store(batch: RuntimeEventBatch): RuntimeIngestionResult {
        val outcome = transactions.required {
            if (!store.claimBatch(batch)) return@required StoredOutcome(accepted = false)
            val original = store.lockProjection(batch.nodeId)
            var projected = original

            val inserted = batch.events.map { event -> event to store.appendEvent(batch.nodeId, event) }
            inserted.filter { (event, accepted) -> accepted && event.sequence == 0L }
                .forEach { (event) -> projected = projected.apply(event) }
            batch.snapshot?.let { snapshot -> projected = projected.replace(snapshot) }
            inserted.filter { (event, accepted) -> accepted && event.sequence > 0L }
                .sortedBy { (event) -> event.sequence }
                .forEach { (event) -> projected = projected.apply(event) }

            if (projected == original) return@required StoredOutcome(accepted = true)
            val versioned = projected.nextVersion()
            store.saveProjection(versioned)
            StoredOutcome(accepted = true, projection = versioned)
        }
        outcome.projection?.let(notifier::changed)
        return RuntimeIngestionResult(outcome.accepted, outcome.projection != null)
    }

    private data class StoredOutcome(
        val accepted: Boolean,
        val projection: RuntimeProjection? = null,
    )
}

class RuntimeProjectionRebuildService(
    private val store: RuntimeEventStore,
    private val replay: RuntimeReplayStore,
    private val transactions: TransactionRunner,
    private val notifier: RuntimeProjectionNotifier,
) : RuntimeProjectionRebuild {
    override fun rebuild(nodeId: String): RuntimeProjection {
        val rebuilt = transactions.required {
            val material = replay.material(nodeId)
                ?: throw ResourceConflict("runtime replay requires an authoritative snapshot for node $nodeId")
            val current = store.lockProjection(nodeId)
            var projection = RuntimeProjection(nodeId = nodeId, version = current.version).replace(material.snapshot)
            material.events.sortedWith(compareBy<RuntimeEvent> { it.sequence }.thenBy { it.occurredAt })
                .forEach { event -> projection = projection.apply(event) }
            val versioned = projection.nextVersion()
            store.saveProjection(versioned)
            versioned
        }
        notifier.changed(rebuilt)
        return rebuilt
    }
}
