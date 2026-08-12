package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.provisioning.domain.model.ConfigRevision
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.testing.TestCatalogs
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith

class CatalogResourceServiceTest {
    private val now = Instant.parse("2026-08-12T12:00:00Z")

    @Test
    fun `put user preserves managed key and advances resource and catalog versions`() {
        val commands = Commands(TestCatalogs.catalog())
        val service = CatalogResourceService(commands, commands, Clock.fixed(now, ZoneOffset.UTC))

        val result = service.putUser(
            "node-1", "user-1", 1,
            UserInput("Renamed", null, null, ResourceState.DISABLED, 3, 4), "operator",
        )

        val user = result.catalog.users.single()
        assertEquals(TestCatalogs.key(2), user.privateKey)
        assertEquals(2, user.version)
        assertEquals(2, result.catalog.version)
        assertEquals("user.updated", commands.cause)
    }

    @Test
    fun `assigned board cannot be removed implicitly`() {
        val commands = Commands(TestCatalogs.catalog())
        val service = CatalogResourceService(commands, commands, Clock.fixed(now, ZoneOffset.UTC))

        assertFailsWith<ResourceConflict> { service.removeBoard("node-1", "board-1", 1, "operator") }
    }

    private class Commands(var catalog: Catalog) : CatalogQueries, CatalogCommands {
        var cause: String? = null
        override fun get(nodeId: String): Catalog = catalog
        override fun create(catalog: Catalog, actor: String): CatalogMutationResult = error("not used")
        override fun replace(
            catalog: Catalog,
            expectedVersion: Long,
            actor: String,
            cause: String,
        ): CatalogMutationResult {
            this.catalog = catalog
            this.cause = cause
            return CatalogMutationResult(
                catalog,
                ConfigRevision(catalog.node.id, 2, 1, catalog.version, byteArrayOf(), "hash", cause, catalog.updatedAt),
                true,
            )
        }
    }
}
