package ru.zevsus.proxy.boardvpn.domain.repository

import ru.zevsus.proxy.boardvpn.domain.model.InstalledApplication

fun interface InstalledApplicationsRepository {
    suspend fun getInstalledApplications(): List<InstalledApplication>
}
