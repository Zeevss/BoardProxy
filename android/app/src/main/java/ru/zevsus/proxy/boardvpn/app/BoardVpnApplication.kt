package ru.zevsus.proxy.boardvpn.app

import android.app.Application

class BoardVpnApplication : Application() {
    val container: AppContainer by lazy(LazyThreadSafetyMode.NONE) {
        AppContainer(applicationContext)
    }
}
