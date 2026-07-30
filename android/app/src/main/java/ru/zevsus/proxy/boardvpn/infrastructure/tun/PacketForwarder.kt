package ru.zevsus.proxy.boardvpn.infrastructure.tun

interface PacketForwarder {
    suspend fun start(tunFileDescriptor: Int, socksAddress: String)

    suspend fun awaitTermination()

    suspend fun stop()

    fun closeImmediately()
}
