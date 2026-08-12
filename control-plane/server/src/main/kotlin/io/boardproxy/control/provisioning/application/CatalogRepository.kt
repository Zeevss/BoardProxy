package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Catalog

interface CatalogRepository {
    fun find(nodeId: String): Catalog?
    fun search(query: String?, offset: Int, limit: Int): List<Catalog>
    fun count(query: String?): Long
    fun create(catalog: Catalog)
    fun replace(catalog: Catalog, expectedVersion: Long): Boolean
}
