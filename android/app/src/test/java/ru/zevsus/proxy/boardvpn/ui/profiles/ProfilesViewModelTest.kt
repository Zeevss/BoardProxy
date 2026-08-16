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
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxySubscriptionUrl
import ru.zevsus.proxy.boardvpn.domain.model.SubscriptionKeySummary
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSubscription
import ru.zevsus.proxy.boardvpn.domain.repository.ResolvedSubscription
import ru.zevsus.proxy.boardvpn.domain.repository.SubscriptionRepository
import ru.zevsus.proxy.boardvpn.domain.subscription.SubscriptionSyncManager
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
    private val subscriptions = object : SubscriptionRepository {
        override suspend fun resolve(
            url: BoardProxySubscriptionUrl,
            preferredKeyId: String?,
        ) = ResolvedSubscription(
            name = "Family",
            selectedKeylink = BoardProxyKeylink.fromRaw(rawKey),
            selectedKeyId = "one",
            metadata = VpnSubscription(
                url = url,
                id = "family",
                revision = "r1",
                keys = listOf(SubscriptionKeySummary("one", "Germany", "node-1", "enabled", 0)),
                selectedKeyId = "one",
            ),
        )
    }

    private fun TestScope.viewModel(profiles: InMemoryVpnProfileRepository) =
        ProfilesViewModel(
            profiles,
            subscriptions,
            SubscriptionSyncManager(
                scope = backgroundScope,
                profiles = profiles,
                subscriptions = subscriptions,
                intervalMillis = Long.MAX_VALUE,
            ),
        )

    @Test
    fun `manual profile is created and selected when nothing is selected yet`() = runTest {
        val profiles = InMemoryVpnProfileRepository()
        val viewModel = viewModel(profiles)
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
        val viewModel = viewModel(profiles)
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
        val viewModel = viewModel(profiles)
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
        val viewModel = viewModel(profiles)
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
        val viewModel = viewModel(profiles)
        collectState(viewModel)

        viewModel.importLink(null)
        runCurrent()
        assertEquals(ProfilesMessage.ClipboardEmpty, viewModel.uiState.value.message)

        viewModel.importLink("not-a-key")
        runCurrent()
        assertEquals(ProfilesMessage.InvalidLink, viewModel.uiState.value.message)

        viewModel.importLink("$rawKey#Clipboard")
        runCurrent()
        assertEquals(ProfilesMessage.ProfileImported, viewModel.uiState.value.message)
        assertEquals(listOf("Clipboard"), viewModel.uiState.value.profiles.map { it.name })
    }

    @Test
    fun `subscription URL creates one grouped profile with resolved keys`() = runTest {
        val profiles = InMemoryVpnProfileRepository()
        val viewModel = viewModel(profiles)
        collectState(viewModel)

        viewModel.importLink("https://subscribe.example.com/s/family#bp1=demo")
        runCurrent()

        val saved = viewModel.uiState.value.profiles.single()
        assertEquals("Family", saved.name)
        assertEquals("family", saved.subscription?.id)
        assertEquals(listOf("Germany"), saved.subscription?.keys?.map { it.name })
    }

    private fun TestScope.collectState(viewModel: ProfilesViewModel) {
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            viewModel.uiState.collect()
        }
        runCurrent()
    }
}
