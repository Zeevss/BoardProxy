package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.NodeConfigInput

/**
 * Чистая функция: одинаковый вход обязан давать одинаковые байты. Публикация
 * сравнивает sha256 результата с текущей, поэтому недетерминированность здесь
 * означала бы поток ложных ревизий и лишних перезапусков ядра.
 */
fun interface CoreConfigCompiler {
    fun compile(input: NodeConfigInput): ByteArray
}
