package ru.zevsus.proxy.boardvpn.ui.routing

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingMode
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingPolicy
import ru.zevsus.proxy.boardvpn.domain.model.InstalledApplication
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState
import ru.zevsus.proxy.boardvpn.domain.repository.AppSettingsRepository
import ru.zevsus.proxy.boardvpn.domain.repository.InstalledApplicationsRepository
import ru.zevsus.proxy.boardvpn.domain.repository.VpnRepository

data class AppRoutingUiState(
    val mode: AppRoutingMode = AppRoutingMode.ExcludeSelectedApps,
    val allProxy: Boolean = true,
    val applications: List<InstalledApplication> = emptyList(),
    val selectedPackageNames: Set<String> = emptySet(),
    val query: String = "",
    val loading: Boolean = true,
    val restartRequired: Boolean = false,
) {
    val visibleApplications: List<InstalledApplication>
        get() {
            val normalizedQuery = query.trim()
            if (normalizedQuery.isEmpty()) return applications
            return applications.filter {
                it.label.contains(normalizedQuery, ignoreCase = true) ||
                    it.packageName.contains(normalizedQuery, ignoreCase = true)
            }
        }

    val selectedCount: Int
        get() = selectedPackageNames.size
}

sealed interface AppRoutingAction {
    data class SelectMode(val mode: AppRoutingMode) : AppRoutingAction
    data class ToggleApplication(val packageName: String) : AppRoutingAction
    data class Search(val query: String) : AppRoutingAction
    data object SelectAllApplications : AppRoutingAction
    data object ClearApplicationSelection : AppRoutingAction
    data object RestartProxy : AppRoutingAction
}

class AppRoutingViewModel(
    private val settingsRepository: AppSettingsRepository,
    private val applicationsRepository: InstalledApplicationsRepository,
    private val vpnRepository: VpnRepository,
) : ViewModel() {
    private val applications = MutableStateFlow<List<InstalledApplication>>(emptyList())
    private val draft = MutableStateFlow<RoutingDraft?>(null)
    private val query = MutableStateFlow("")
    private val loading = MutableStateFlow(true)

    private val routingSource = combine(
        settingsRepository.observeSettings(),
        vpnRepository.observeSession(),
    ) { settings, session ->
        RoutingSource(settings.appRoutingPolicy, session)
    }

    val uiState = combine(
        routingSource,
        applications,
        draft,
        query,
        loading,
    ) { source, applications, draft, query, loading ->
        val policy = source.policy
        val installedPackages = applications.mapTo(mutableSetOf()) { it.packageName }
        val unavailableSelections = policy.packageNames
            .asSequence()
            .filterNot(installedPackages::contains)
            .map { packageName ->
                InstalledApplication(
                    packageName = packageName,
                    label = packageName,
                    installed = false,
                )
            }
            .toList()

        AppRoutingUiState(
            mode = draft?.mode ?: policy.mode.selectedMode(),
            allProxy = draft?.allProxy ?: policy.allProxy,
            applications = unavailableSelections + applications,
            selectedPackageNames = policy.packageNames,
            query = query,
            loading = loading,
            restartRequired = source.restartRequired,
        )
    }.stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000),
        initialValue = AppRoutingUiState(),
    )

    init {
        loadApplications()
    }

    fun onAction(action: AppRoutingAction) {
        when (action) {
            is AppRoutingAction.Search -> query.value = action.query
            is AppRoutingAction.SelectMode -> selectMode(action.mode)
            is AppRoutingAction.ToggleApplication -> toggleApplication(action.packageName)
            AppRoutingAction.SelectAllApplications -> selectAllApplications()
            AppRoutingAction.ClearApplicationSelection -> replaceSelection(emptySet())
            AppRoutingAction.RestartProxy -> restartProxy()
        }
    }

    private fun selectMode(mode: AppRoutingMode) {
        val state = uiState.value
        if (mode == AppRoutingMode.AllApps) {
            persist(
                AppRoutingPolicy(
                    mode = state.mode.selectedMode(),
                    packageNames = state.selectedPackageNames,
                    allProxy = true,
                )
            )
            return
        }

        draft.value = RoutingDraft(mode = mode, allProxy = false)
        if (state.selectedPackageNames.isNotEmpty()) {
            persist(
                AppRoutingPolicy(
                    mode = mode,
                    packageNames = state.selectedPackageNames,
                    allProxy = false,
                )
            )
        }
    }

    private fun toggleApplication(packageName: String) {
        val state = uiState.value
        val updated = state.selectedPackageNames.toMutableSet().apply {
            if (!add(packageName)) remove(packageName)
        }
        replaceSelection(updated)
    }

    private fun selectAllApplications() {
        val installedPackages = uiState.value.applications
            .asSequence()
            .filter(InstalledApplication::installed)
            .mapTo(mutableSetOf(), InstalledApplication::packageName)
        replaceSelection(uiState.value.selectedPackageNames + installedPackages)
    }

    private fun replaceSelection(packageNames: Set<String>) {
        val state = uiState.value
        val mode = (draft.value?.mode ?: state.mode).selectedMode()
        val allProxy = draft.value?.allProxy ?: state.allProxy
        persist(
            AppRoutingPolicy(
                mode = mode,
                packageNames = packageNames,
                allProxy = allProxy || packageNames.isEmpty(),
            )
        )
    }

    private fun persist(policy: AppRoutingPolicy) {
        draft.value = RoutingDraft(
            mode = policy.mode.selectedMode(),
            allProxy = policy.allProxy,
        )
        viewModelScope.launch {
            settingsRepository.setAppRoutingPolicy(policy)
        }
    }

    private fun restartProxy() {
        if (!uiState.value.restartRequired) return
        viewModelScope.launch { vpnRepository.restart() }
    }

    private fun loadApplications() {
        loading.value = true
        viewModelScope.launch {
            applications.value = runCatching {
                applicationsRepository.getInstalledApplications()
            }.getOrDefault(emptyList())
            loading.value = false
        }
    }

    private data class RoutingDraft(
        val mode: AppRoutingMode,
        val allProxy: Boolean,
    )

    private data class RoutingSource(
        val policy: AppRoutingPolicy,
        val session: VpnSessionState,
    ) {
        val restartRequired: Boolean
            get() {
                val active = session as? VpnSessionState.Active ?: return false
                val running = active.phase == VpnSessionPhase.Connected ||
                    active.phase is VpnSessionPhase.Reconnecting
                return running &&
                    active.appliedAppRoutingPolicy != null &&
                    active.appliedAppRoutingPolicy != policy
            }
    }
}

private fun AppRoutingMode.selectedMode(): AppRoutingMode = when (this) {
    AppRoutingMode.AllApps -> AppRoutingMode.ExcludeSelectedApps
    AppRoutingMode.OnlySelectedApps,
    AppRoutingMode.ExcludeSelectedApps,
    -> this
}
