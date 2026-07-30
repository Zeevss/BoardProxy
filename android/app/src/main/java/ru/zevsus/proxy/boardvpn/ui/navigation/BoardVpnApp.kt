package ru.zevsus.proxy.boardvpn.ui.navigation

import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import ru.zevsus.proxy.boardvpn.ui.home.HomeRoute
import ru.zevsus.proxy.boardvpn.ui.home.HomeViewModel
import ru.zevsus.proxy.boardvpn.ui.profiles.ProfilesRoute
import ru.zevsus.proxy.boardvpn.ui.profiles.ProfilesViewModel
import ru.zevsus.proxy.boardvpn.ui.routing.AppRoutingScreen
import ru.zevsus.proxy.boardvpn.ui.routing.AppRoutingViewModel
import ru.zevsus.proxy.boardvpn.ui.settings.SettingsRoute
import ru.zevsus.proxy.boardvpn.ui.settings.SettingsViewModel

private const val TAB_TRANSITION_MILLIS = 220

@Composable
fun BoardVpnApp(
    homeViewModel: HomeViewModel,
    profilesViewModel: ProfilesViewModel,
    settingsViewModel: SettingsViewModel,
    appRoutingViewModel: AppRoutingViewModel,
    onConnectRequest: () -> Unit,
    onClipboardImportRequest: () -> Unit,
    onOpenSystemVpnSettings: () -> Unit,
    appVersion: String,
    navController: NavHostController = rememberNavController(),
) {
    val snackbarHostState = remember { SnackbarHostState() }
    val backStackEntry by navController.currentBackStackEntryAsState()
    val currentDestination = backStackEntry?.destination
    val showBottomBar = BoardVpnTab.entries.any { tab ->
        currentDestination?.hasRoute(tab.route::class) == true
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbarHostState) },
        bottomBar = {
            if (showBottomBar) {
                NavigationBar {
                    BoardVpnTab.entries.forEach { tab ->
                        val selected = currentDestination?.hasRoute(tab.route::class) == true
                        NavigationBarItem(
                            selected = selected,
                            onClick = { navController.switchTab(tab) },
                            icon = { Icon(tab.icon, contentDescription = null) },
                            label = { Text(stringResource(tab.labelRes)) },
                        )
                    }
                }
            }
        },
    ) { contentPadding ->
        NavHost(
            navController = navController,
            startDestination = HomeDestination,
            enterTransition = { slideIntoTab() },
            exitTransition = { fadeOut(tween(TAB_TRANSITION_MILLIS)) },
            popEnterTransition = { slideIntoTab() },
            popExitTransition = { fadeOut(tween(TAB_TRANSITION_MILLIS)) },
        ) {
            composable<HomeDestination> {
                HomeRoute(
                    viewModel = homeViewModel,
                    snackbarHostState = snackbarHostState,
                    contentPadding = contentPadding,
                    onConnectRequest = onConnectRequest,
                    onManageProfiles = { navController.switchTab(BoardVpnTab.Profiles) },
                )
            }
            composable<ProfilesDestination> {
                ProfilesRoute(
                    viewModel = profilesViewModel,
                    snackbarHostState = snackbarHostState,
                    contentPadding = contentPadding,
                    onClipboardImportRequest = onClipboardImportRequest,
                )
            }
            composable<SettingsDestination> {
                SettingsRoute(
                    viewModel = settingsViewModel,
                    contentPadding = contentPadding,
                    onOpenSystemVpnSettings = onOpenSystemVpnSettings,
                    onOpenAppRouting = { navController.navigate(AppRoutingDestination) },
                    appVersion = appVersion,
                )
            }
            composable<AppRoutingDestination> {
                val routingState by appRoutingViewModel.uiState.collectAsStateWithLifecycle()
                AppRoutingScreen(
                    state = routingState,
                    onAction = appRoutingViewModel::onAction,
                    onBack = navController::popBackStack,
                )
            }
        }
    }
}

private fun AnimatedContentTransitionScope<*>.slideIntoTab() =
    fadeIn(tween(TAB_TRANSITION_MILLIS)) +
        slideInHorizontally(tween(TAB_TRANSITION_MILLIS)) { width -> width / 12 }

private fun NavHostController.switchTab(tab: BoardVpnTab) {
    navigate(tab.route) {
        popUpTo(graph.findStartDestination().id) { saveState = true }
        launchSingleTop = true
        restoreState = true
    }
}
