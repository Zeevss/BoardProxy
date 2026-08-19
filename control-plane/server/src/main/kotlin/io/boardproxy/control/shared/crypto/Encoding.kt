package io.boardproxy.control.shared.crypto

import java.security.MessageDigest
import java.util.Base64

/** Кодировки, которые до этого объявлялись приватно в каждом втором файле. */

fun ByteArray.base64(): String = Base64.getEncoder().encodeToString(this)

fun ByteArray.base64Url(): String = Base64.getUrlEncoder().withoutPadding().encodeToString(this)

fun ByteArray.sha256Hex(): String =
    MessageDigest.getInstance("SHA-256").digest(this).joinToString("") { "%02x".format(it) }

fun String.sha256Hex(): String = toByteArray(Charsets.UTF_8).sha256Hex()
