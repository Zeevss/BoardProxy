package ru.zevsus.proxy.boardvpn.infrastructure.applications

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import ru.zevsus.proxy.boardvpn.domain.model.InstalledApplication
import ru.zevsus.proxy.boardvpn.domain.repository.InstalledApplicationsRepository

class PackageManagerInstalledApplicationsRepository(
    private val context: Context,
) : InstalledApplicationsRepository {
    override suspend fun getInstalledApplications(): List<InstalledApplication> =
        withContext(Dispatchers.IO) {
            val packageManager = context.packageManager
            val intent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
            val activities = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                packageManager.queryIntentActivities(
                    intent,
                    PackageManager.ResolveInfoFlags.of(0),
                )
            } else {
                @Suppress("DEPRECATION")
                packageManager.queryIntentActivities(intent, 0)
            }

            activities
                .asSequence()
                .map { resolveInfo ->
                    InstalledApplication(
                        packageName = resolveInfo.activityInfo.packageName,
                        label = resolveInfo.loadLabel(packageManager).toString()
                            .ifBlank { resolveInfo.activityInfo.packageName },
                    )
                }
                .filterNot { it.packageName == context.packageName }
                .distinctBy(InstalledApplication::packageName)
                .sortedWith(
                    compareBy(String.CASE_INSENSITIVE_ORDER, InstalledApplication::label)
                )
                .toList()
        }
}
