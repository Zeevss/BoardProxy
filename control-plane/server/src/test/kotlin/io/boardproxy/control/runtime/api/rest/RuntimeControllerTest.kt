package io.boardproxy.control.runtime.api.rest

import io.boardproxy.control.runtime.application.RuntimeEventView
import io.boardproxy.control.runtime.application.RuntimeQueries
import io.boardproxy.control.runtime.application.RuntimeProjectionRebuild
import io.boardproxy.control.runtime.domain.RuntimeProjection
import io.boardproxy.control.runtime.domain.RuntimeSessionState
import io.boardproxy.control.runtime.domain.RuntimeUserState
import io.boardproxy.control.shared.errors.ApiExceptionHandler
import org.springframework.test.web.servlet.get
import org.springframework.test.web.servlet.setup.MockMvcBuilders
import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals

class RuntimeControllerTest {
    private val now = Instant.parse("2026-08-12T10:00:00Z")

    @Test
    fun `projection exposes explicit session completeness`() {
        val queries = Queries(
            RuntimeProjection(
                nodeId = "node-1", coreBootId = "boot-1", lastSequence = 7,
                runtimeRevision = 2, gapDetected = false, capturedAt = now,
                users = mapOf("alice" to RuntimeUserState("alice", true, activeSessions = 2)),
                sessions = mapOf("known" to RuntimeSessionState("known", "alice", "main", now)),
                sessionDetailsComplete = false, version = 3,
            ),
        )
        val mockMvc = MockMvcBuilders.standaloneSetup(controller(queries))
            .setControllerAdvice(ApiExceptionHandler())
            .build()

        mockMvc.get("/api/v1/nodes/node-1/runtime")
            .andExpect {
                status { isOk() }
                jsonPath("$.lastSequence") { value(7) }
                jsonPath("$.sessionDetailsComplete") { value(false) }
                jsonPath("$.users[0].activeSessions") { value(2) }
                jsonPath("$.sessions[0].bundleId") { value("known") }
            }
    }

    @Test
    fun `event cursor requires boot identity`() {
        val mockMvc = MockMvcBuilders.standaloneSetup(controller(Queries(null)))
            .setControllerAdvice(ApiExceptionHandler())
            .build()

        mockMvc.get("/api/v1/nodes/node-1/runtime/events") {
            param("afterSequence", "3")
        }.andExpect {
            status { isBadRequest() }
            content { string(org.hamcrest.Matchers.containsString("requires a non-blank coreBootId")) }
        }
    }

    @Test
    fun `event query forwards cursor and bounded limit`() {
        val queries = Queries(null)
        val controller = controller(queries)

        controller.events("node-1", "boot-1", 3, 25)

        assertEquals(QueryCall("node-1", "boot-1", 3, 25), queries.call)
    }

    private fun controller(queries: RuntimeQueries) = RuntimeController(
        queries,
        RuntimeProjectionRebuild { nodeId -> RuntimeProjection(nodeId) },
    )

    private class Queries(private val value: RuntimeProjection?) : RuntimeQueries {
        var call: QueryCall? = null
        override fun projection(nodeId: String): RuntimeProjection? = value
        override fun events(
            nodeId: String,
            coreBootId: String?,
            afterSequence: Long?,
            limit: Int,
        ): List<RuntimeEventView> {
            call = QueryCall(nodeId, coreBootId, afterSequence, limit)
            return emptyList()
        }
    }

    private data class QueryCall(
        val nodeId: String,
        val coreBootId: String?,
        val afterSequence: Long?,
        val limit: Int,
    )
}
