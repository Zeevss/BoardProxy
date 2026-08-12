package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.AssignedUser
import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceNotFound
import java.time.Clock

class CatalogHistoryService(
    private val catalogs: CatalogQueries,
    private val snapshots: CatalogSnapshotRepository,
    private val commands: CatalogCommands,
    private val clock: Clock,
) : CatalogHistoryQueries, CatalogHistoryCommands {
    override fun history(nodeId: String, offset: Int, limit: Int): CatalogHistoryPage =
        CatalogHistoryPage(snapshots.list(nodeId, offset, limit), offset, limit, snapshots.count(nodeId))

    override fun diff(nodeId: String, fromVersion: Long, toVersion: Long): CatalogDiff {
        if (fromVersion <= 0 || toVersion <= 0) throw InvalidRequest("catalog versions must be positive")
        val before = snapshot(nodeId, fromVersion)
        val after = snapshot(nodeId, toVersion)
        return CatalogDiff(nodeId, fromVersion, toVersion, CatalogDiffer.diff(before, after))
    }

    override fun rollback(
        nodeId: String,
        catalogVersion: Long,
        expectedVersion: Long,
        actor: String,
    ): CatalogMutationResult {
        val current = catalogs.get(nodeId)
        if (current.version != expectedVersion) throw ResourceConflict("catalog $nodeId version changed")
        val historical = snapshot(nodeId, catalogVersion)
        val now = clock.instant()
        val boardVersions = current.boards.associate { it.id to it.version }
        val userVersions = current.users.associate { it.id to it.version }
        val restored = historical.copy(
            node = historical.node.copy(version = current.node.version + 1, updatedAt = now),
            boards = historical.boards.map { board ->
                board.copy(version = (boardVersions[board.id] ?: 0) + 1, updatedAt = now)
            },
            users = historical.users.map { user ->
                user.copy(version = (userVersions[user.id] ?: 0) + 1, updatedAt = now)
            },
            assignment = historical.assignment.copy(version = current.assignment.version + 1, updatedAt = now),
            version = current.version + 1,
            updatedAt = now,
        )
        return commands.replace(restored, expectedVersion, actor, "catalog.rolled-back")
    }

    private fun snapshot(nodeId: String, version: Long): Catalog =
        snapshots.find(nodeId, version)
            ?: throw ResourceNotFound("catalog $nodeId version $version not found")
}

internal object CatalogDiffer {
    fun diff(before: Catalog, after: Catalog): List<CatalogChange> {
        val changes = mutableListOf<CatalogChange>()
        change(changes, "node.name", before.node.name, after.node.name)
        change(changes, "node.state", before.node.state.name.lowercase(), after.node.state.name.lowercase())
        change(changes, "node.core.idleTimeout", before.node.core.server.idleTimeout, after.node.core.server.idleTimeout)
        change(
            changes,
            "node.core.allowPrivateEgress",
            before.node.core.server.allowPrivateEgress,
            after.node.core.server.allowPrivateEgress,
        )
        diffBoards(changes, before, after)
        diffUsers(changes, before, after)
        change(changes, "assignment.boardIds", before.assignment.boardIds.sorted(), after.assignment.boardIds.sorted())
        change(
            changes,
            "assignment.users",
            assignments(before.assignment.users),
            assignments(after.assignment.users),
        )
        return changes.sortedBy(CatalogChange::path)
    }

    private fun diffBoards(changes: MutableList<CatalogChange>, before: Catalog, after: Catalog) {
        val left = before.boards.associateBy { it.id }
        val right = after.boards.associateBy { it.id }
        (left.keys + right.keys).forEach { id ->
            val old = left[id]
            val new = right[id]
            change(changes, "boards.$id.name", old?.name, new?.name)
            change(changes, "boards.$id.hash", old?.hash, new?.hash)
            change(changes, "boards.$id.state", old?.state?.name?.lowercase(), new?.state?.name?.lowercase())
            change(changes, "boards.$id.maxLanes", old?.maxLanes, new?.maxLanes)
        }
    }

    private fun diffUsers(changes: MutableList<CatalogChange>, before: Catalog, after: Catalog) {
        val left = before.users.associateBy { it.id }
        val right = after.users.associateBy { it.id }
        (left.keys + right.keys).forEach { id ->
            val old = left[id]
            val new = right[id]
            change(changes, "users.$id.name", old?.name, new?.name)
            change(changes, "users.$id.state", old?.state?.name?.lowercase(), new?.state?.name?.lowercase())
            change(changes, "users.$id.maxSessions", old?.maxSessions, new?.maxSessions)
            change(changes, "users.$id.maxLanes", old?.maxLanes, new?.maxLanes)
            change(changes, "users.$id.credential", old?.credentialType(), new?.credentialType())
        }
    }

    private fun assignments(users: List<AssignedUser>) = users.sortedBy { it.userId }
        .associate { it.userId to it.boardIds.sorted() }

    private fun io.boardproxy.control.provisioning.domain.model.User.credentialType() =
        if (privateKey != null) "managed-private-key" else "external-public-key"

    private fun change(changes: MutableList<CatalogChange>, path: String, before: Any?, after: Any?) {
        if (before != after) changes += CatalogChange(path, before?.toString(), after?.toString())
    }
}
