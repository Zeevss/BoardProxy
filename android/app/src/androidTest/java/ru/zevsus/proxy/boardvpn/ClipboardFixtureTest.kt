package ru.zevsus.proxy.boardvpn

import android.content.ClipData
import android.content.ClipboardManager
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.platform.app.InstrumentationRegistry
import java.net.HttpURLConnection
import java.net.URL
import org.junit.Rule
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import ru.zevsus.proxy.boardvpn.app.MainActivity

/** Device-only fixture for live testing clipboard import without a production debug intent. */
class ClipboardFixtureTest {
    @get:Rule
    val composeRule = createAndroidComposeRule<MainActivity>()

    @Test
    fun setClipboardFromInstrumentationArgument() {
        setClipboardFromArgument()
    }

    @Test
    fun importsClipboardArgumentWhileAppIsForeground() {
        val keylink = setClipboardFromArgument()
        val expectedName = keylink.substringAfter('#', "Imported profile")
            .ifBlank { "Imported profile" }

        composeRule.onNodeWithText("Add key from clipboard").performClick()
        composeRule.onNodeWithText(expectedName).assertExists()
        composeRule.onNodeWithText("Connect").assertExists()
    }

    @Test
    fun connectsAndForwardsTcpFromClipboardArgument() {
        setClipboardFromArgument()
        composeRule.onNodeWithText("Add key from clipboard").performClick()
        composeRule.onNodeWithText("Connect").performClick()

        composeRule.waitUntil(timeoutMillis = CONNECTION_TIMEOUT_MILLIS) {
            composeRule.onAllNodesWithText("Connected").fetchSemanticsNodes().isNotEmpty()
        }

        val connection = URL(TEST_URL).openConnection() as HttpURLConnection
        connection.connectTimeout = HTTP_TIMEOUT_MILLIS
        connection.readTimeout = HTTP_TIMEOUT_MILLIS
        try {
            assertEquals(200, connection.responseCode)
        } finally {
            connection.disconnect()
        }

        composeRule.onNodeWithText("Disconnect").performClick()
        composeRule.waitUntil(timeoutMillis = CONNECTION_TIMEOUT_MILLIS) {
            composeRule.onAllNodesWithText("Disconnected").fetchSemanticsNodes().isNotEmpty()
        }
    }

    private fun setClipboardFromArgument(): String {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val value = InstrumentationRegistry.getArguments().getString(KEY_ARGUMENT).orEmpty()
        assertTrue("Pass -e $KEY_ARGUMENT <keylink>", value.isNotBlank())

        instrumentation.runOnMainSync {
            val clipboard = instrumentation.targetContext
                .getSystemService(ClipboardManager::class.java)
            clipboard.setPrimaryClip(ClipData.newPlainText("BoardProxy key", value))
        }
        return value
    }

    private companion object {
        const val KEY_ARGUMENT = "boardproxyKeylink"
        const val TEST_URL = "https://example.com"
        const val CONNECTION_TIMEOUT_MILLIS = 30_000L
        const val HTTP_TIMEOUT_MILLIS = 15_000
    }
}
