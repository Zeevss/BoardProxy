package ru.zevsus.proxy.boardvpn.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

private val Default = Typography()

/**
 * Minimal scale: light, wide headlines for status and timers, plain body text
 * everywhere else.
 */
val Typography = Typography(
    displaySmall = Default.displaySmall.copy(
        fontFamily = FontFamily.Default,
        fontWeight = FontWeight.Light,
        letterSpacing = (-1).sp,
    ),
    headlineMedium = Default.headlineMedium.copy(
        fontWeight = FontWeight.Medium,
        letterSpacing = (-0.5).sp,
    ),
    headlineSmall = Default.headlineSmall.copy(
        fontWeight = FontWeight.Light,
        letterSpacing = 1.sp,
    ),
    titleMedium = Default.titleMedium.copy(fontWeight = FontWeight.SemiBold),
    titleSmall = Default.titleSmall.copy(fontWeight = FontWeight.Medium),
    bodyLarge = TextStyle(
        fontFamily = FontFamily.Default,
        fontWeight = FontWeight.Normal,
        fontSize = 16.sp,
        lineHeight = 24.sp,
        letterSpacing = 0.5.sp,
    ),
    labelLarge = Default.labelLarge.copy(fontWeight = FontWeight.Medium),
    labelMedium = Default.labelMedium.copy(letterSpacing = 0.8.sp),
    labelSmall = Default.labelSmall.copy(letterSpacing = 1.sp),
)
