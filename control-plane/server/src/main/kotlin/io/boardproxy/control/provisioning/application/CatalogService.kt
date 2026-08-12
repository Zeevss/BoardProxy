package io.boardproxy.control.provisioning.application

import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.audit.domain.AuditEvent
import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.events.OutboxEvent
import io.boardproxy.control.shared.events.OutboxRepository
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Clock
import java.util.UUID

class CatalogService(
    private val catalogs: CatalogRepository,
    private val snapshots: CatalogSnapshotRepository,
    private val compiler: CoreConfigCompiler,
    private val revisions: ConfigRevisionRepository,
    private val audit: AuditRepository,
    private val outbox: OutboxRepository,
    private val transactions: TransactionRunner,
    private val clock: Clock,
    private val nextId: () -> String = { UUID.randomUUID().toString() },
) : CatalogCommands, CatalogQueries {
    override fun create(catalog: Catalog, actor: String): CatalogMutationResult {
        requireActor(actor)
        if (catalog.version != 1L) throw InvalidRequest("new catalog version must be 1")
        val result = transactions.required {
            if (catalogs.find(catalog.node.id) != null) {
                throw ResourceConflict("catalog ${catalog.node.id} already exists")
            }
            catalogs.create(catalog)
            snapshots.save(catalog)
            publish(catalog, actor, "catalog.created")
        }
        return result
    }

    override fun replace(
        catalog: Catalog,
        expectedVersion: Long,
        actor: String,
        cause: String,
    ): CatalogMutationResult {
        requireActor(actor)
        if (expectedVersion < 1 || catalog.version != expectedVersion + 1) {
            throw InvalidRequest("replacement catalog version must equal expectedVersion + 1")
        }
        val result = transactions.required {
            val current = catalogs.find(catalog.node.id) ?: run {
                throw ResourceNotFound("catalog ${catalog.node.id} not found")
            }
            snapshots.save(current)
            if (!catalogs.replace(catalog, expectedVersion)) {
                throw ResourceConflict("catalog ${catalog.node.id} version changed")
            }
            snapshots.save(catalog)
            publish(catalog, actor, cause)
        }
        return result
    }

    override fun get(nodeId: String): Catalog =
        catalogs.find(nodeId) ?: throw ResourceNotFound("catalog $nodeId not found")

    fun search(query: String?, offset: Int, limit: Int): CatalogPage {
        val items = catalogs.search(query, offset, limit).map { catalog ->
            CatalogSummary(
                catalog.node.id, catalog.node.name, catalog.node.state.name.lowercase(),
                catalog.boards.size, catalog.users.size, catalog.version, catalog.updatedAt,
            )
        }
        return CatalogPage(items, offset, limit, catalogs.count(query))
    }

    private fun publish(catalog: Catalog, actor: String, cause: String): CatalogMutationResult {
        val now = clock.instant()
        val revision = revisions.append(
            nodeId = catalog.node.id,
            catalogVersion = catalog.version,
            configToml = compiler.compile(catalog),
            cause = cause,
            createdAt = now,
        )
        audit.append(
            AuditEvent(
                id = nextId(), nodeId = catalog.node.id, actor = actor, action = cause,
                resourceType = "catalog", resourceId = catalog.node.id,
                resourceVersion = catalog.version, catalogVersion = catalog.version,
                occurredAt = now,
            ),
        )
        val configChanged = revision.catalogVersion == catalog.version
        if (configChanged) {
            outbox.append(
                OutboxEvent(
                    id = nextId(), aggregateType = "node", aggregateId = catalog.node.id,
                    type = "desired-state.changed",
                    payload = mapOf(
                        "nodeId" to catalog.node.id,
                        "catalogVersion" to catalog.version,
                        "desiredRevision" to revision.revision,
                        "configSha256" to revision.configSha256,
                    ),
                    occurredAt = now,
                ),
            )
        }
        return CatalogMutationResult(catalog, revision, configChanged)
    }

    private fun requireActor(actor: String) {
        if (actor.isBlank()) throw InvalidRequest("actor is required")
    }

}
