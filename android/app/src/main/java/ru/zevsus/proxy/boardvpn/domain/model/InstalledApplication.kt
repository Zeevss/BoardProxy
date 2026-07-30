package ru.zevsus.proxy.boardvpn.domain.model

data class InstalledApplication(
    val packageName: String,
    val label: String,
    val installed: Boolean = true,
)
