package io.boardproxy.control.provisioning.api.rest

import io.boardproxy.control.provisioning.application.CatalogCommands
import io.boardproxy.control.provisioning.application.CatalogMutationResult
import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.shared.errors.ApiExceptionHandler
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.testing.TestCatalogs
import org.hamcrest.Matchers.containsString
import org.hamcrest.Matchers.not
import org.springframework.test.web.servlet.get
import org.springframework.test.web.servlet.setup.MockMvcBuilders
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import java.security.Principal
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith

class CatalogControllerTest {
    private val now = Instant.parse("2026-02-03T04:05:06Z")

    @Test
    fun `GET exposes catalog but never private keys`() {
        val catalog = TestCatalogs.catalog(now = now)
        val controller = CatalogController(NoCommands, CatalogQueries { catalog }, Clock.fixed(now, ZoneOffset.UTC))
        val mockMvc = MockMvcBuilders.standaloneSetup(controller)
            .setControllerAdvice(ApiExceptionHandler())
            .build()

        mockMvc.get("/api/v1/catalogs/node-1")
            .andExpect {
                status { isOk() }
                header { string("ETag", "\"1\"") }
                content { string(not(containsString(catalog.node.core.server.privateKey))) }
                content { string(not(containsString(requireNotNull(catalog.users.single().privateKey)))) }
                jsonPath("$.node.core.serverPrivateKey") { doesNotExist() }
                jsonPath("$.users[0].privateKey") { doesNotExist() }
            }
    }

    @Test
    fun `replacement preserves omitted secrets`() {
        val current = TestCatalogs.catalog(now = now)
        val request = CatalogWriteRequest(
            node = NodeWriteRequest("node-1", "Renamed", core = CoreSettingsWriteRequest()),
            boards = listOf(BoardWriteRequest("board-1", "Main", "board-hash", maxLanes = 4)),
            users = listOf(UserWriteRequest("user-1", "Alice", maxSessions = 2, maxLanes = 3)),
            assignment = AssignmentWriteRequest(
                listOf("board-1"),
                listOf(AssignedUserWriteRequest("user-1", listOf("board-1"))),
            ),
        )

        val replacement = request.toReplacement(now.plusSeconds(1), current)

        assertEquals(current.node.core.server.privateKey, replacement.node.core.server.privateKey)
        assertEquals(current.users.single().privateKey, replacement.users.single().privateKey)
    }

    @Test
    fun `stale If-Match is reported as a version conflict`() {
        val current = TestCatalogs.catalog(version = 2, now = now)
        val controller = CatalogController(NoCommands, CatalogQueries { current }, Clock.fixed(now, ZoneOffset.UTC))

        assertFailsWith<ResourceConflict> {
            controller.replace("node-1", "1", writeRequest(), Principal { "operator" })
        }
    }

    private fun writeRequest() = CatalogWriteRequest(
        node = NodeWriteRequest("node-1", "Primary", core = CoreSettingsWriteRequest()),
        boards = listOf(BoardWriteRequest("board-1", "Main", "board-hash", maxLanes = 4)),
        users = listOf(UserWriteRequest("user-1", "Alice", maxSessions = 2, maxLanes = 3)),
        assignment = AssignmentWriteRequest(
            listOf("board-1"),
            listOf(AssignedUserWriteRequest("user-1", listOf("board-1"))),
        ),
    )

    private object NoCommands : CatalogCommands {
        override fun create(catalog: Catalog, actor: String): CatalogMutationResult = unsupported()
        override fun replace(
            catalog: Catalog,
            expectedVersion: Long,
            actor: String,
            cause: String,
        ): CatalogMutationResult = unsupported()

        private fun unsupported(): Nothing = error("not used")
    }
}
