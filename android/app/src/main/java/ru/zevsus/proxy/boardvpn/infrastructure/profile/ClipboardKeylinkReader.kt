package ru.zevsus.proxy.boardvpn.infrastructure.profile

import android.content.ClipboardManager
import android.content.Context

class ClipboardKeylinkReader(
    private val context: Context,
) {
    private val clipboard = context.getSystemService(ClipboardManager::class.java)

    fun readText(): String? = clipboard.primaryClip
        ?.takeIf { it.itemCount > 0 }
        ?.getItemAt(0)
        ?.coerceToText(context)
        ?.toString()
        ?.trim()
        ?.takeIf(String::isNotEmpty)
}
