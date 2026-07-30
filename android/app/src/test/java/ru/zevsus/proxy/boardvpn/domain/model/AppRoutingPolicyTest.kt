package ru.zevsus.proxy.boardvpn.domain.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AppRoutingPolicyTest {
    @Test
    fun `all apps is the safe default`() {
        assertTrue(AppRoutingPolicy.AllApps.allProxy)
        assertEquals(AppRoutingMode.ExcludeSelectedApps, AppRoutingPolicy.AllApps.mode)
        assertEquals(emptySet<String>(), AppRoutingPolicy.AllApps.packageNames)
    }

    @Test(expected = IllegalArgumentException::class)
    fun `selected mode rejects an empty application list`() {
        AppRoutingPolicy(mode = AppRoutingMode.OnlySelectedApps)
    }

    @Test
    fun `all proxy preserves the previous mode and application list`() {
        val policy = AppRoutingPolicy(
            mode = AppRoutingMode.OnlySelectedApps,
            packageNames = setOf("com.example.app"),
            allProxy = true,
        )

        assertTrue(policy.allProxy)
        assertEquals(AppRoutingMode.OnlySelectedApps, policy.mode)
        assertEquals(setOf("com.example.app"), policy.packageNames)
    }

    @Test(expected = IllegalArgumentException::class)
    fun `invalid package name is rejected`() {
        AppRoutingPolicy(
            mode = AppRoutingMode.ExcludeSelectedApps,
            packageNames = setOf("not a package"),
        )
    }
}
