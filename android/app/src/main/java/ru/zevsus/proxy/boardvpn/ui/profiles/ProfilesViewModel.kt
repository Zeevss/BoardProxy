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
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.repository.VpnProfileRepository

class ProfilesViewModel(
    private val profileRepository: VpnProfileRepository,
) : ViewModel() {
    private val editorState = MutableStateFlow<ProfileEditorState?>(null)
    private val deletionRequest = MutableStateFlow<VpnProfileId?>(null)
    private val message = MutableStateFlow<ProfilesMessage?>(null)

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
    }.stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000),
        initialValue = ProfilesUiState(),
    )

    fun onAction(action: ProfilesAction) {
        when (action) {
            ProfilesAction.AddProfile -> editorState.value = ProfileEditorState()
            ProfilesAction.ImportFromClipboard -> Unit // handled by the hosting Activity
            ProfilesAction.DismissEditor -> editorState.value = null
            ProfilesAction.SaveEditor -> saveEditor()
            ProfilesAction.ConfirmDeletion -> confirmDeletion()
            ProfilesAction.DismissDeletion -> deletionRequest.value = null
            ProfilesAction.DismissMessage -> message.value = null
            is ProfilesAction.SelectProfile -> selectProfile(action.profileId)
            is ProfilesAction.EditProfile -> openEditor(action.profileId)
            is ProfilesAction.RequestDeletion -> deletionRequest.value = action.profileId
            is ProfilesAction.EditorNameChanged -> editorState.update {
                it?.copy(name = action.name, nameError = false)
            }
            is ProfilesAction.EditorKeylinkChanged -> editorState.update {
                it?.copy(keylink = action.keylink, keylinkError = false)
            }
        }
    }

    /** Imports the raw clipboard text read by the Activity. */
    fun importKeylink(rawValue: String?) {
        if (rawValue.isNullOrBlank()) {
            message.value = ProfilesMessage.ClipboardEmpty
            return
        }

        val profile = runCatching {
            VpnProfile.fromKeylink(BoardProxyKeylink.fromRaw(rawValue.trim()))
        }.getOrElse {
            message.value = ProfilesMessage.InvalidKeylink
            return
        }

        viewModelScope.launch {
            profileRepository.saveProfile(profile)
            profileRepository.selectProfile(profile.id)
            message.value = ProfilesMessage.ProfileImported
        }
    }

    private fun openEditor(profileId: VpnProfileId) {
        val profile = uiState.value.profiles.firstOrNull { it.id == profileId } ?: return
        editorState.value = ProfileEditorState(
            profileId = profile.id,
            name = profile.name,
            keylink = profile.keylink.reveal(),
        )
    }

    private fun saveEditor() {
        val editor = editorState.value ?: return
        val name = editor.name.trim()
        val keylink = runCatching { BoardProxyKeylink.fromRaw(editor.keylink.trim()) }.getOrNull()

        if (name.isEmpty() || keylink == null) {
            editorState.value = editor.copy(
                nameError = name.isEmpty(),
                keylinkError = keylink == null,
            )
            return
        }

        val profile = VpnProfile(
            id = editor.profileId ?: VpnProfile.fromKeylink(keylink).id,
            name = name,
            keylink = keylink,
        )

        viewModelScope.launch {
            profileRepository.saveProfile(profile)
            if (uiState.value.selectedProfileId == null) {
                profileRepository.selectProfile(profile.id)
            }
            editorState.value = null
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
}
