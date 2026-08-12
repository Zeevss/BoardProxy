package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.provisioning.domain.model.ConfigRevision

data class CatalogMutationResult(
    val catalog: Catalog,
    val desiredRevision: ConfigRevision,
    val configChanged: Boolean,
)

interface CatalogCommands {
    fun create(catalog: Catalog, actor: String): CatalogMutationResult
    fun replace(
        catalog: Catalog,
        expectedVersion: Long,
        actor: String,
        cause: String = "catalog.replaced",
    ): CatalogMutationResult
}

fun interface CatalogQueries {
    fun get(nodeId: String): Catalog
}

data class CatalogSummary(
    val nodeId: String,
    val name: String,
    val state: String,
    val boards: Int,
    val users: Int,
    val version: Long,
    val updatedAt: java.time.Instant,
)

data class CatalogPage(val items: List<CatalogSummary>, val offset: Int, val limit: Int, val total: Long)

fun interface CatalogOverviewQueries {
    fun search(query: String?, offset: Int, limit: Int): CatalogPage
}
