package io.boardproxy.control.provisioning.domain.model

import io.boardproxy.control.shared.crypto.X25519
import io.boardproxy.control.shared.crypto.base64
import java.security.SecureRandom

/**
 * Приватные ключи выпускает хаб, а не оператор: ключ, пришедший снаружи, нельзя
 * считать сгенерированным честным источником случайности.
 */
fun generatePrivateKey(random: SecureRandom = SecureRandom()): String =
    "base64:" + X25519.generatePrivateKey(random).base64()
