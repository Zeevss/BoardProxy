package ru.zevsus.proxy.boardvpn.ui.components

import java.util.Locale
import kotlin.time.Duration

private const val KILOBYTE = 1_024.0
private const val MEGABYTE = 1_024 * 1_024.0
private const val GIGABYTE = 1_024 * 1_024 * 1_024.0

fun formatBytes(bytes: Long): String = when {
    bytes >= GIGABYTE -> "%.1f GB".localeFormat(bytes / GIGABYTE)
    bytes >= MEGABYTE -> "%.1f MB".localeFormat(bytes / MEGABYTE)
    bytes >= KILOBYTE -> "%.0f KB".localeFormat(bytes / KILOBYTE)
    else -> "$bytes B"
}

fun formatBytesPerSecond(bytesPerSecond: Long): String = when {
    bytesPerSecond >= MEGABYTE -> "%.1f MB/s".localeFormat(bytesPerSecond / MEGABYTE)
    bytesPerSecond >= KILOBYTE -> "%.0f KB/s".localeFormat(bytesPerSecond / KILOBYTE)
    else -> "$bytesPerSecond B/s"
}

/** `mm:ss` below an hour, `h:mm:ss` above it. */
fun formatDuration(duration: Duration): String {
    val totalSeconds = duration.inWholeSeconds.coerceAtLeast(0)
    val hours = totalSeconds / 3_600
    val minutes = (totalSeconds % 3_600) / 60
    val seconds = totalSeconds % 60

    return if (hours > 0) {
        String.format(Locale.US, "%d:%02d:%02d", hours, minutes, seconds)
    } else {
        String.format(Locale.US, "%02d:%02d", minutes, seconds)
    }
}

/** Short, non-secret profile identity shown next to profile names. */
fun formatFingerprint(fingerprint: String): String = fingerprint
    .take(12)
    .chunked(4)
    .joinToString(separator = " ")

private fun String.localeFormat(value: Double): String = String.format(Locale.US, this, value)
