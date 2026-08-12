package io.boardproxy.control.shared.config

import io.boardproxy.control.shared.security.AesGcmSecretCipher
import io.boardproxy.control.shared.security.KeyringSecretCipher
import io.boardproxy.control.shared.security.SecretCipher
import org.springframework.beans.factory.annotation.Value
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration

@Configuration
class SecurityConfiguration {
    @Bean
    fun secretCipher(
        @Value("\${control.security.master-key}") masterKey: String,
        @Value("\${control.security.master-key-id}") keyId: String,
        @Value("\${control.security.master-keys:}") configuredKeyring: String,
    ): SecretCipher {
        if (configuredKeyring.isBlank()) return AesGcmSecretCipher(masterKey, keyId)
        val keys = configuredKeyring.split(';').filter(String::isNotBlank).associate { entry ->
            val separator = entry.indexOf(':')
            require(separator > 0 && separator < entry.lastIndex) {
                "CONTROL_MASTER_KEYS entries must use key-id:base64 format"
            }
            entry.substring(0, separator).trim() to entry.substring(separator + 1).trim()
        }
        return KeyringSecretCipher(keyId, keys)
    }
}
