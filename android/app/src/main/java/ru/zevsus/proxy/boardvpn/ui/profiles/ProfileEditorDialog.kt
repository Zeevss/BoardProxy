package ru.zevsus.proxy.boardvpn.ui.profiles

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import ru.zevsus.proxy.boardvpn.R

@Composable
fun ProfileEditorDialog(
    state: ProfileEditorState,
    onNameChange: (String) -> Unit,
    onKeylinkChange: (String) -> Unit,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = {
            Text(
                stringResource(
                    if (state.isNew) R.string.profiles_add else R.string.profiles_edit_title
                )
            )
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(
                    value = state.name,
                    onValueChange = onNameChange,
                    label = { Text(stringResource(R.string.profiles_field_name)) },
                    singleLine = true,
                    isError = state.nameError,
                    supportingText = if (state.nameError) {
                        { Text(stringResource(R.string.profiles_error_name)) }
                    } else {
                        null
                    },
                    keyboardOptions = KeyboardOptions(
                        capitalization = KeyboardCapitalization.Sentences,
                        imeAction = ImeAction.Next,
                    ),
                    enabled = !state.resolving,
                    modifier = Modifier.fillMaxWidth(),
                )

                OutlinedTextField(
                    value = state.keylink,
                    onValueChange = onKeylinkChange,
                    label = { Text(stringResource(R.string.profiles_field_key)) },
                    placeholder = { Text(stringResource(R.string.profiles_field_key_hint)) },
                    isError = state.keylinkError,
                    supportingText = if (state.keylinkError) {
                        { Text(stringResource(R.string.profiles_error_key)) }
                    } else {
                        null
                    },
                    minLines = 2,
                    maxLines = 4,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                    enabled = !state.resolving,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            TextButton(onClick = onConfirm, enabled = !state.resolving) {
                Text(
                    stringResource(
                        if (state.resolving) R.string.profiles_resolving else R.string.action_save
                    )
                )
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(stringResource(R.string.action_cancel))
            }
        },
    )
}
