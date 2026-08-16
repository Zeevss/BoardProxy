package ru.zevsus.proxy.boardvpn.domain.subscription

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.repository.SubscriptionRepository
import ru.zevsus.proxy.boardvpn.domain.repository.VpnProfileRepository

data class SubscriptionSyncState(
    val refreshing: Set<VpnProfileId> = emptySet(),
    val failed: Set<VpnProfileId> = emptySet(),
)

data class SubscriptionSyncReport(
    val updated: Set<VpnProfileId>,
    val failed: Set<VpnProfileId>,
)

/**
 * Single synchronization path used by app startup, the periodic timer,
 * manual refresh, and the VPN runtime.
 */
class SubscriptionSyncManager(
    private val scope: CoroutineScope,
    private val profiles: VpnProfileRepository,
    private val subscriptions: SubscriptionRepository,
    private val intervalMillis: Long = DEFAULT_INTERVAL_MILLIS,
    private val nowMillis: () -> Long = System::currentTimeMillis,
) {
    private val mutex = Mutex()
    private val mutableState = MutableStateFlow(SubscriptionSyncState())
    private var periodicJob: Job? = null

    val state: StateFlow<SubscriptionSyncState> = mutableState.asStateFlow()

    /** Immediately refreshes once, then repeats while the application process is alive. */
    fun start() {
        if (periodicJob != null) return
        periodicJob = scope.launch {
            refreshAll()
            while (isActive) {
                delay(intervalMillis)
                refreshAll()
            }
        }
    }

    suspend fun refreshAll(): SubscriptionSyncReport {
        val ids = profiles.observeProfiles().first()
            .filter { it.subscription != null }
            .mapTo(linkedSetOf(), VpnProfile::id)
        return refresh(ids)
    }

    suspend fun refresh(profileId: VpnProfileId): Result<VpnProfile> {
        val report = refresh(setOf(profileId))
        val profile = profiles.getProfile(profileId)
        return if (profileId in report.updated && profile != null) {
            Result.success(profile)
        } else {
            Result.failure(IllegalStateException("Subscription update failed"))
        }
    }

    /** Avoids a duplicate request when startup/import has just refreshed this profile. */
    suspend fun refreshIfStale(
        profileId: VpnProfileId,
        maxAgeMillis: Long = CONNECT_FRESHNESS_MILLIS,
    ): Result<VpnProfile> {
        val report = refresh(setOf(profileId), maxAgeMillis)
        val profile = profiles.getProfile(profileId)
        return if (profileId in report.updated && profile != null) {
            Result.success(profile)
        } else {
            Result.failure(IllegalStateException("Subscription update failed"))
        }
    }

    private suspend fun refresh(
        ids: Set<VpnProfileId>,
        maxAgeMillis: Long? = null,
    ): SubscriptionSyncReport = mutex.withLock {
        if (ids.isEmpty()) return@withLock SubscriptionSyncReport(emptySet(), emptySet())

        val profilesById = linkedMapOf<VpnProfileId, VpnProfile?>()
        ids.forEach { id -> profilesById[id] = profiles.getProfile(id) }
        val failed = profilesById
            .filterValues { it?.subscription == null }
            .keys
            .toMutableSet()
        val fresh = if (maxAgeMillis == null) {
            emptySet()
        } else {
            profilesById.filterValues { profile ->
                val updatedAt = profile?.subscription?.updatedAtEpochMillis ?: 0L
                updatedAt > 0L && nowMillis() - updatedAt <= maxAgeMillis
            }.keys
        }
        val refreshIds = ids - failed - fresh
        mutableState.value = mutableState.value.copy(
            refreshing = refreshIds,
            failed = mutableState.value.failed - ids,
        )
        val updated = fresh.toMutableSet()
        refreshIds.forEach { id ->
            val profile = checkNotNull(profilesById[id])
            val subscription = checkNotNull(profile.subscription)
            runCatching {
                subscriptions.resolve(
                    url = subscription.url,
                    preferredKeyId = subscription.selectedKeyId.ifBlank { null },
                )
            }
                .onSuccess { resolved ->
                    profiles.saveProfile(
                        profile.copy(
                            keylink = resolved.selectedKeylink,
                            subscription = resolved.metadata.copy(
                                updatedAtEpochMillis = nowMillis(),
                            ),
                        )
                    )
                    updated += id
                }
                .onFailure { failed += id }
        }
        mutableState.value = SubscriptionSyncState(
            refreshing = emptySet(),
            failed = (mutableState.value.failed - updated) + failed,
        )
        SubscriptionSyncReport(updated, failed)
    }

    companion object {
        const val DEFAULT_INTERVAL_MINUTES = 15L
        const val DEFAULT_INTERVAL_MILLIS = DEFAULT_INTERVAL_MINUTES * 60 * 1_000
        const val CONNECT_FRESHNESS_MILLIS = 30_000L
    }
}
