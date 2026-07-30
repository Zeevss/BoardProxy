package ru.zevsus.proxy.boardvpn.ui.profiles

import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId

/** Open editor dialog, either creating a profile or changing an existing one. */
data class ProfileEditorState(
    val profileId: VpnProfileId? = null,
    val name: String = "",
    val keylink: String = "",
    val nameError: Boolean = false,
    val keylinkError: Boolean = false,
) {
    val isNew: Boolean
        get() = profileId == null
}

sealed interface ProfilesMessage {
    data object ClipboardEmpty : ProfilesMessage
    data object InvalidKeylink : ProfilesMessage
    data object ProfileImported : ProfilesMessage
    data object ProfileDeleted : ProfilesMessage
}

data class ProfilesUiState(
    val profiles: List<VpnProfile> = emptyList(),
    val selectedProfileId: VpnProfileId? = null,
    val editor: ProfileEditorState? = null,
    val profilePendingDeletion: VpnProfile? = null,
    val message: ProfilesMessage? = null,
)

sealed interface ProfilesAction {
    data object AddProfile : ProfilesAction
    data object ImportFromClipboard : ProfilesAction
    data object DismissEditor : ProfilesAction
    data object SaveEditor : ProfilesAction
    data object ConfirmDeletion : ProfilesAction
    data object DismissDeletion : ProfilesAction
    data object DismissMessage : ProfilesAction

    data class SelectProfile(val profileId: VpnProfileId) : ProfilesAction
    data class EditProfile(val profileId: VpnProfileId) : ProfilesAction
    data class RequestDeletion(val profileId: VpnProfileId) : ProfilesAction
    data class EditorNameChanged(val name: String) : ProfilesAction
    data class EditorKeylinkChanged(val keylink: String) : ProfilesAction
}
