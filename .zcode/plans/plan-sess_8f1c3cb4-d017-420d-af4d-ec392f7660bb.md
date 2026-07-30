## UI: три экрана, минимализм + анимации

### 0. Зависимости
- `libs.versions.toml`: `navigation = "2.8.9"` → `androidx-navigation-compose` (ветка 2.8.x совместима с Compose BOM 2024.09; 2.9.x тянет Compose 1.8 и конфликтует с BOM).
- Иконки: только `Icons.*` из material-icons-core + собственный щит. Новых графических зависимостей нет.

### 1. Тема (`ui/theme`)
- `Color.kt`: свой минималистичный набор — глубокий индиго + бирюзовый акцент, отдельные `lightColorScheme`/`darkColorScheme` (используются, когда Material You выключен или Android < 12).
- `Theme.kt`: `BoardVPNTheme(themeMode: ThemeMode = System, dynamicColor: Boolean = true)`; тёмный/светлый режим выбирается настройкой, а не только системой.
- `Type.kt`: дозаполняю шкалу (display/headline/title/label) — крупные тонкие цифры для статистики, moderate letter-spacing.
- Адаптивность: размер щита и отступы считаются от `BoxWithConstraints` (min(width, height) * 0.55, потолок 260.dp), так что на планшетах/ландшафте не разъезжается.

### 2. Настройки: домен + хранение
- `domain/model/AppSettings.kt`: `@Serializable data class AppSettings(themeMode: ThemeMode = System, dynamicColor: Boolean = true, autoConnectOnLaunch: Boolean = false)` + `enum ThemeMode { System, Light, Dark }`.
- `domain/repository/AppSettingsRepository.kt`: `observeSettings()`, `suspend fun setThemeMode/setDynamicColor/setAutoConnectOnLaunch`.
- `infrastructure/persistence`: `DataStoreAppSettingsRepository` + `AppSettingsSerializer` над отдельным файлом `app_settings.json` (тот же паттерн, что и профили: `CorruptionException` → дефолт).
- Экран показывает только то, что реально работает: тема, Material You, автоподключение, ссылка в системные VPN-настройки, «О приложении» (версия). Сетевые поля (SOCKS-порт, UDP, LocalDNS, DNS, bypass) — следующим шагом, в этом диффе их нет.

### 3. Навигация (`ui/navigation`)
- `BoardVpnDestination.kt` — `@Serializable` объекты `Home`, `Profiles`, `Settings` (type-safe routes).
- `BoardVpnApp.kt` — `Scaffold` + `NavigationBar` (щит / список / шестерёнка), `NavHost` с плавным `fadeIn + slideInHorizontally` между вкладками, `saveState/restoreState` при переключении.
- `MainActivity` собирает настройки, оборачивает всё в `BoardVPNTheme(...)` и раздаёт три ViewModel через существующий `viewModelFactory`.

### 4. Главный экран (`ui/home`)
- `ShieldButton.kt`: щит рисую сам (`ImageVector` c `Path`), вокруг — круглая кнопка:
  - цвет фона/обводки анимируется `animateColorAsState` по статусу (серый → акцент → зелёный → янтарный при reconnect);
  - при `Connecting/Reconnecting` — пульсирующее кольцо (`rememberInfiniteTransition`, scale+alpha);
  - нажатие даёт spring-отклик; повторный тап при подключении = disconnect.
- Под кнопкой: статус (`AnimatedContent`) и таймер сессии `00:14:32` — `HomeViewModel` фиксирует момент перехода в `Connected` и тикает раз в секунду (только пока экран подписан).
- Ряд статистики: ↓ скорость · пинг · ↑ скорость (`downloadBytesPerSecond`, `roundTripTimeMillis`, `uploadBytesPerSecond`), значения меняются через `AnimatedContent`, в простое — «—».
- Внизу карточка-селектор профиля: имя + короткий фингерпринт; тап открывает `ModalBottomSheet` со списком профилей и кнопкой «Manage profiles» (переход на вкладку профилей).
- Ошибки (`HomeProblem`) — `Snackbar` вместо красного текста, с действием «Dismiss».

### 5. Экран профилей (`ui/profiles`)
- `ProfilesViewModel` + `ProfilesUiState`: список карточек (имя, фингерпринт, отметка «выбран»), состояние диалога-редактора, ошибки валидации.
- Карточка: тап — сделать активным, меню (`⋮`) — «Rename», «Delete»; появление/удаление через `animateItem()`.
- `ProfileEditorDialog`: поля «Name» и «Key (bproxy://…)»; для существующего профиля ключ можно заменить, ошибки парсинга показываются под полем. Тот же диалог используется для ручного добавления.
- Кнопки: FAB «Add profile» + кнопка «Paste from clipboard» (чтение буфера остаётся в Activity, как сейчас для Home).
- Удаление подтверждается `AlertDialog`; пустой список — аккуратный empty-state со щитом и подсказкой.

### 6. Экран настроек (`ui/settings`)
- `SettingsViewModel` поверх `AppSettingsRepository`.
- Секции: **Appearance** (сегментированный переключатель System/Light/Dark + свитч Material You, скрыт на Android < 12), **Connection** (свитч автоподключения), **System** (кнопка «Android VPN settings» → `Settings.ACTION_VPN_SETTINGS`), **About** (имя, версия, ядро BoardProxy).
- Изменения применяются сразу и переживают перезапуск.

### 7. Автоподключение
- В `MainActivity` при холодном старте (`savedInstanceState == null`): если `autoConnectOnLaunch`, сессия `Disconnected` и профиль выбран — один раз запускается тот же путь, что и по кнопке (с запросом разрешения при необходимости).

### 8. Строки и локализация
- Все новые тексты в `values/strings.xml`; добавляю `values-ru/strings.xml` с полным переводом (включая уже существующие ключи).

### 9. Тесты и проверка
- `ProfilesViewModelTest`: добавление/переименование/удаление/выбор, невалидный ключ → ошибка в диалоге, импорт из буфера.
- `SettingsViewModelTest`: переключатели пишутся в репозиторий.
- `DataStoreAppSettingsRepositoryTest` + `AppSettingsSerializerTest` (fake `DataStore`, как у профилей).
- `HomeViewModelTest`: дополняю проверкой таймера/статистики в состоянии.
- Compose-превью для каждого экрана в светлой и тёмной теме.
- Прогон `./gradlew :app:compileDebugKotlin :app:testDebugUnitTest` (+ `:app:assembleDebug`), отчёт по результату.

### 10. Документация
- `ARCHITECTURE.md`: обновляю дерево пакетов (`ui/navigation`, `ui/profiles`, `ui/settings`, `ui/components`), фиксирую добавление navigation-compose в dependency policy, отмечаю stage 4 и добавляю пункт про хранение настроек; отдельно помечаю, что сетевые настройки (порт, UDP, LocalDNS, DNS, bypass, split-tunneling) — следующий этап.
