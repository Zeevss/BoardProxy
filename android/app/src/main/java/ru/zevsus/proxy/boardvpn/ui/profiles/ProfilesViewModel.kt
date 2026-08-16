package ru.zevsus.proxy.boardvpn.ui.profiles

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxySubscriptionUrl
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.repository.VpnProfileRepository
import ru.zevsus.proxy.boardvpn.domain.repository.SubscriptionRepository
import ru.zevsus.proxy.boardvpn.domain.subscription.SubscriptionSyncManager

class ProfilesViewModel(
    private val profileRepository: VpnProfileRepository,
    private val subscriptionRepository: SubscriptionRepository,
    private val subscriptionSyncManager: SubscriptionSyncManager,
) : ViewModel() {
    private val editorState = MutableStateFlow<ProfileEditorState?>(null)
    private val deletionRequest = MutableStateFlow<VpnProfileId?>(null)
    private val message = MutableStateFlow<ProfilesMessage?>(null)
    private val shareRequest = MutableStateFlow<VpnProfileId?>(null)

    val uiState = combine(
        profileRepository.observeProfiles(),
        profileRepository.observeSelectedProfileId(),
        editorState,
        deletionRequest,
        message,
    ) { profiles, selectedProfileId, editor, pendingDeletion, message ->
        ProfilesUiState(
            profiles = profiles,
            selectedProfileId = selectedProfileId,
            editor = editor,
            profilePendingDeletion = profiles.firstOrNull { it.id == pendingDeletion },
            message = message,
        )
    }.combine(subscriptionSyncManager.state) { state, sync ->
        state.copy(
            refreshingSubscriptions = sync.refreshing,
            failedSubscriptions = sync.failed,
        )
    }.combine(shareRequest) { state, sharing ->
        state.copy(profileForSharing = state.profiles.firstOrNull { it.id == sharing })
    }.stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000),
        initialValue = ProfilesUiState(),
    )

    fun onAction(action: ProfilesAction) {
        when (action) {
            ProfilesAction.AddProfile -> editorState.value = ProfileEditorState()
            ProfilesAction.ImportFromClipboard -> Unit // handled by the hosting Activity
            ProfilesAction.ScanQr -> Unit // handled by the route
            ProfilesAction.DismissEditor -> editorState.value = null
            ProfilesAction.SaveEditor -> saveEditor()
            ProfilesAction.ConfirmDeletion -> confirmDeletion()
            ProfilesAction.DismissDeletion -> deletionRequest.value = null
            ProfilesAction.DismissMessage -> message.value = null
            ProfilesAction.DismissShare -> shareRequest.value = null
            ProfilesAction.RefreshSubscriptions -> refreshSubscriptions()
            is ProfilesAction.SelectProfile -> selectProfile(action.profileId)
            is ProfilesAction.EditProfile -> openEditor(action.profileId)
            is ProfilesAction.RequestDeletion -> deletionRequest.value = action.profileId
            is ProfilesAction.ShareProfile -> shareRequest.value = action.profileId
            is ProfilesAction.RefreshSubscription -> refreshSubscription(action.profileId)
            is ProfilesAction.EditorNameChanged -> editorState.update {
                it?.copy(name = action.name, nameError = false)
            }
            is ProfilesAction.EditorKeylinkChanged -> editorState.update {
                it?.copy(keylink = action.keylink, keylinkError = false)
            }
        }
    }

    /** Imports the raw clipboard text read by the Activity. */
    fun importLink(rawValue: String?) {
        if (rawValue.isNullOrBlank()) {
            message.value = ProfilesMessage.ClipboardEmpty
            return
        }

        viewModelScope.launch {
            runCatching { resolveProfile(rawValue.trim(), name = "", profileId = null) }
                .onSuccess { profile ->
                    profileRepository.saveProfile(profile)
                    profileRepository.selectProfile(profile.id)
                    message.value = ProfilesMessage.ProfileImported
                }
                .onFailure {
                    message.value = if (isSubscriptionUrl(rawValue)) {
                        ProfilesMessage.SubscriptionFailed
                    } else {
                        ProfilesMessage.InvalidLink
                    }
                }
        }
    }

    private fun openEditor(profileId: VpnProfileId) {
        val profile = uiState.value.profiles.firstOrNull { it.id == profileId } ?: return
        editorState.value = ProfileEditorState(
            profileId = profile.id,
            name = profile.name,
            keylink = profile.subscription?.url?.reveal() ?: profile.keylink.reveal(),
        )
    }

    private fun saveEditor() {
        val editor = editorState.value ?: return
        val name = editor.name.trim()
        val rawLink = editor.keylink.trim()
        val linkValid = runCatching { BoardProxyKeylink.fromRaw(rawLink) }.isSuccess ||
            isSubscriptionUrl(rawLink)
        if (name.isEmpty() || !linkValid) {
            editorState.value = editor.copy(
                nameError = name.isEmpty(),
                keylinkError = !linkValid,
            )
            return
        }

        editorState.value = editor.copy(resolving = true, keylinkError = false)
        viewModelScope.launch {
            runCatching { resolveProfile(rawLink, name, editor.profileId) }
                .onSuccess { profile ->
                    profileRepository.saveProfile(profile)
                    if (uiState.value.selectedProfileId == null) {
                        profileRepository.selectProfile(profile.id)
                    }
                    editorState.value = null
                }
                .onFailure {
                    editorState.value = editor.copy(keylinkError = true, resolving = false)
                }
        }
    }

    private fun confirmDeletion() {
        val profileId = deletionRequest.value ?: return
        viewModelScope.launch {
            profileRepository.deleteProfile(profileId)
            deletionRequest.value = null
            message.value = ProfilesMessage.ProfileDeleted
        }
    }

    private fun selectProfile(profileId: VpnProfileId) {
        viewModelScope.launch { profileRepository.selectProfile(profileId) }
    }

    private fun refreshSubscriptions() {
        viewModelScope.launch {
            val report = subscriptionSyncManager.refreshAll()
            message.value = if (report.failed.isEmpty()) {
                ProfilesMessage.SubscriptionsUpdated
            } else {
                ProfilesMessage.SubscriptionUpdateFailed
            }
        }
    }

    private fun refreshSubscription(profileId: VpnProfileId) {
        viewModelScope.launch {
            message.value = subscriptionSyncManager.refresh(profileId).fold(
                onSuccess = { ProfilesMessage.SubscriptionsUpdated },
                onFailure = { ProfilesMessage.SubscriptionUpdateFailed },
            )
        }
    }

    private suspend fun resolveProfile(
        rawLink: String,
        name: String,
        profileId: VpnProfileId?,
    ): VpnProfile {
        val direct = runCatching { BoardProxyKeylink.fromRaw(rawLink) }.getOrNull()
        if (direct != null) {
            val imported = VpnProfile.fromKeylink(direct)
            return VpnProfile(
                id = profileId ?: imported.id,
                name = name.ifBlank { imported.name },
                keylink = direct,
            )
        }
        val url = BoardProxySubscriptionUrl.fromRaw(rawLink)
        val resolved = subscriptionRepository.resolve(url)
        return VpnProfile(
            id = profileId ?: VpnProfileId("subscription-${url.fingerprint()}"),
            name = name.ifBlank { resolved.name },
            keylink = resolved.selectedKeylink,
            subscription = resolved.metadata.copy(
                updatedAtEpochMillis = System.currentTimeMillis(),
            ),
        )
    }

    private fun isSubscriptionUrl(raw: String): Boolean =
        runCatching { BoardProxySubscriptionUrl.fromRaw(raw.trim()) }.isSuccess
}
