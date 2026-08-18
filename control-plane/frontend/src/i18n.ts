import type { Language, Section } from './types'

const copy = {
  en: {
    brandSub: 'Control plane', groups: ['Fleet', 'Delivery', 'Insight', 'System'],
    nav: { overview: 'Overview', nodes: 'Nodes', users: 'Users', boards: 'Boards', traffic: 'Traffic', activity: 'Activity', settings: 'Settings' } satisfies Record<Section, string>,
    search: 'Search nodes, users, boards', live: 'Live', reconnecting: 'Reconnecting', allNodes: 'All nodes',
    titles: {
      overview: ['Fleet overview', 'Real-time health, desired-state drift and traffic across the fleet.'],
      nodes: ['Nodes', 'Enrollment, desired-state drift and runtime facts per node.'],
      users: ['Users', 'A user is created with its access scope and limits in one step — the subscription is derived from it by the backend.'],
      boards: ['Boards', 'Boards are grouped by node — a node can run several boards at once.'],
      traffic: ['Traffic', 'Interface counters and per-user payload are stored and queried separately.'],
      activity: ['Activity', 'Authenticated SSE-backed runtime and control-plane updates.'],
      settings: ['Settings', 'Subscription delivery and control-plane runtime. Both apply without redeploying nodes.'],
    } satisfies Record<Section, [string, string]>,
    soleAdmin: 'sole operator account', empty: 'No data yet', loading: 'Loading control plane', logout: 'Sign out', refresh: 'Refresh', cancel: 'Cancel', save: 'Save', create: 'Create', remove: 'Remove', revoke: 'Revoke', enable: 'Enable', disable: 'Disable', details: 'Details', copied: 'Copied', copy: 'Copy',
  },
  ru: {
    brandSub: 'Панель управления', groups: ['Флот', 'Доставка', 'Аналитика', 'Система'],
    nav: { overview: 'Обзор', nodes: 'Ноды', users: 'Пользователи', boards: 'Борды', traffic: 'Трафик', activity: 'События', settings: 'Настройки' } satisfies Record<Section, string>,
    search: 'Поиск нод, пользователей, бордов', live: 'Онлайн', reconnecting: 'Переподключение', allNodes: 'Все ноды',
    titles: {
      overview: ['Обзор флота', 'Здоровье нод, desired-state drift и трафик в реальном времени.'],
      nodes: ['Ноды', 'Регистрация, расхождение desired/applied и runtime-факты по каждой ноде.'],
      users: ['Пользователи', 'Пользователь создаётся вместе с доступом и лимитами за один шаг — подписку бэкенд формирует сам.'],
      boards: ['Борды', 'Борды сгруппированы по нодам — на одной ноде может работать несколько бордов.'],
      traffic: ['Трафик', 'Счётчики интерфейса и пользовательский payload хранятся и запрашиваются раздельно.'],
      activity: ['События', 'Runtime-события и обновления control plane через защищённый SSE-поток.'],
      settings: ['Настройки', 'Доставка подписок и runtime панели. И то, и другое применяется без переустановки нод.'],
    } satisfies Record<Section, [string, string]>,
    soleAdmin: 'единственный аккаунт', empty: 'Данных пока нет', loading: 'Загрузка control plane', logout: 'Выйти', refresh: 'Обновить', cancel: 'Отмена', save: 'Сохранить', create: 'Создать', remove: 'Удалить', revoke: 'Отозвать', enable: 'Включить', disable: 'Выключить', details: 'Подробнее', copied: 'Скопировано', copy: 'Копировать',
  },
} as const

export function t(language: Language) { return copy[language] }
