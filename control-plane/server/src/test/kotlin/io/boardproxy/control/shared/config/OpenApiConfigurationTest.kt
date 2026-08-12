package io.boardproxy.control.shared.config

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull

class OpenApiConfigurationTest {
    @Test
    fun `browser API advertises bearer authentication`() {
        val api = OpenApiConfiguration().controlPlaneOpenApi()

        assertEquals("v1", api.info.version)
        assertEquals("bearer", api.components.securitySchemes["bearerAuth"]?.scheme)
        assertNotNull(api.security.single()["bearerAuth"])
    }
}
