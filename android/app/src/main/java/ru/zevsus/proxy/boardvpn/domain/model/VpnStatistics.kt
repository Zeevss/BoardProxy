package ru.zevsus.proxy.boardvpn.domain.model

data class VpnStatistics(
    val roundTripTimeMillis: Long? = null,
    val activeStreams: Int = 0,
    val uploadedBytes: Long = 0,
    val downloadedBytes: Long = 0,
    val uploadBytesPerSecond: Long = 0,
    val downloadBytesPerSecond: Long = 0,
    val streams: List<ProxyStreamStatistics> = emptyList(),
) {
    companion object {
        val Empty = VpnStatistics()
    }
}

data class ProxyStreamStatistics(
    val id: Long,
    val target: String,
    val uploadedBytes: Long,
    val downloadedBytes: Long,
    val startedAtEpochMillis: Long,
)
