package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Catalog
import java.time.Instant

data class CatalogSnapshotMetadata(
    val nodeId: String,
    val catalogVersion: Long,
    val createdAt: Instant,
)

interface CatalogSnapshotRepository {
    fun save(catalog: Catalog)
    fun find(nodeId: String, catalogVersion: Long): Catalog?
    fun list(nodeId: String, offset: Int, limit: Int): List<CatalogSnapshotMetadata>
    fun count(nodeId: String): Long
}

data class CatalogChange(val path: String, val before: String?, val after: String?)

data class CatalogDiff(
    val nodeId: String,
    val fromVersion: Long,
    val toVersion: Long,
    val changes: List<CatalogChange>,
)

data class CatalogHistoryPage(
    val items: List<CatalogSnapshotMetadata>,
    val offset: Int,
    val limit: Int,
    val total: Long,
)

interface CatalogHistoryQueries {
    fun history(nodeId: String, offset: Int, limit: Int): CatalogHistoryPage
    fun diff(nodeId: String, fromVersion: Long, toVersion: Long): CatalogDiff
}

fun interface CatalogHistoryCommands {
    fun rollback(nodeId: String, catalogVersion: Long, expectedVersion: Long, actor: String): CatalogMutationResult
}
