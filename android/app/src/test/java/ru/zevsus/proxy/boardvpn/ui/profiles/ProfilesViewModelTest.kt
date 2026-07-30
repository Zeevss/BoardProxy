package ru.zevsus.proxy.boardvpn.ui.profiles

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.infrastructure.fake.InMemoryVpnProfileRepository
import ru.zevsus.proxy.boardvpn.test.MainDispatcherRule

@OptIn(ExperimentalCoroutinesApi::class)
class ProfilesViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private val rawKey = "bproxy://${"A".repeat(86)}"
    private val profile = VpnProfile(
        id = VpnProfileId("home"),
        name = "Home",
        keylink = BoardProxyKeylink.fromRaw(rawKey),
    )

    @Test
    fun `manual profile is created and selected when nothing is selected yet`() = runTest {
        val profiles = InMemoryVpnProfileRepository()
        val viewModel = ProfilesViewModel(profiles)
        collectState(viewModel)

        viewModel.onAction(ProfilesAction.AddProfile)
        viewModel.onAction(ProfilesAction.EditorNameChanged("Berlin"))
        viewModel.onAction(ProfilesAction.EditorKeylinkChanged(rawKey))
        viewModel.onAction(ProfilesAction.SaveEditor)
        runCurrent()

        val state = viewModel.uiState.value
        assertNull(state.editor)
        assertEquals(listOf("Berlin"), state.profiles.map { it.name })
        assertEquals(state.profiles.single().id, state.selectedProfileId)
    }

    @Test
    fun `editor reports invalid name and key without saving`() = runTest {
        val profiles = InMemoryVpnProfileRepository()
        val viewModel = ProfilesViewModel(profiles)
        collectState(viewModel)

        viewModel.onAction(ProfilesAction.AddProfile)
        viewModel.onAction(ProfilesAction.EditorKeylinkChanged("not-a-key"))
        viewModel.onAction(ProfilesAction.SaveEditor)
        runCurrent()

        val editor = viewModel.uiState.value.editor
        assertTrue(editor?.nameError == true)
        assertTrue(editor?.keylinkError == true)
        assertTrue(viewModel.uiState.value.profiles.isEmpty())
    }

    @Test
    fun `existing profile can be renamed`() = runTest {
        val profiles = InMemoryVpnProfileRepository(listOf(profile))
        val viewModel = ProfilesViewModel(profiles)
        collectState(viewModel)

        viewModel.onAction(ProfilesAction.EditProfile(profile.id))
        runCurrent()
        viewModel.onAction(ProfilesAction.EditorNameChanged("Home renamed"))
        viewModel.onAction(ProfilesAction.SaveEditor)
        runCurrent()

        assertEquals(listOf("Home renamed"), viewModel.uiState.value.profiles.map { it.name })
        assertEquals(profile.id, viewModel.uiState.value.profiles.single().id)
    }

    @Test
    fun `deletion is confirmed through the dialog`() = runTest {
        val profiles = InMemoryVpnProfileRepository(listOf(profile))
        val viewModel = ProfilesViewModel(profiles)
        collectState(viewModel)

        viewModel.onAction(ProfilesAction.RequestDeletion(profile.id))
        runCurrent()
        assertEquals(profile, viewModel.uiState.value.profilePendingDeletion)

        viewModel.onAction(ProfilesAction.ConfirmDeletion)
        runCurrent()

        assertTrue(viewModel.uiState.value.profiles.isEmpty())
        assertEquals(ProfilesMessage.ProfileDeleted, viewModel.uiState.value.message)
    }

    @Test
    fun `clipboard import reports empty and invalid content`() = runTest {
        val profiles = InMemoryVpnProfileRepository()
        val viewModel = ProfilesViewModel(profiles)
        collectState(viewModel)

        viewModel.importKeylink(null)
        runCurrent()
        assertEquals(ProfilesMessage.ClipboardEmpty, viewModel.uiState.value.message)

        viewModel.importKeylink("not-a-key")
        runCurrent()
        assertEquals(ProfilesMessage.InvalidKeylink, viewModel.uiState.value.message)

        viewModel.importKeylink("$rawKey#Clipboard")
        runCurrent()
        assertEquals(ProfilesMessage.ProfileImported, viewModel.uiState.value.message)
        assertEquals(listOf("Clipboard"), viewModel.uiState.value.profiles.map { it.name })
    }

    private fun TestScope.collectState(viewModel: ProfilesViewModel) {
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            viewModel.uiState.collect()
        }
        runCurrent()
    }
}
