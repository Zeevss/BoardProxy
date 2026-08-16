import type { Language, Section } from './types'

const copy = {
  en: {
    brandSub: 'Control plane', groups: ['Fleet', 'Delivery', 'Insight', 'System'],
    nav: { overview: 'Overview', nodes: 'Nodes', subscriptions: 'Subscriptions', users: 'Users', boards: 'Boards', traffic: 'Traffic', activity: 'Activity', access: 'Access' } satisfies Record<Section, string>,
    search: 'Search nodes, users, subscriptions', live: 'Live', reconnecting: 'Reconnecting', allNodes: 'All nodes',
    titles: {
      overview: ['Fleet overview', 'Real-time health, desired-state drift and traffic across the fleet.'],
      nodes: ['Nodes', 'Enrollment, desired-state drift and runtime facts per node.'],
      subscriptions: ['Subscriptions', 'Stable delivery identities and their node/user bindings.'],
      users: ['Users', 'Per-user credentials, limits and quota usage. Private keys stay write-only.'],
      boards: ['Boards', 'Board hashes, lane limits, runtime lifecycle and node placement.'],
      traffic: ['Traffic', 'Interface counters and per-user payload are stored and queried separately.'],
      activity: ['Activity', 'Authenticated SSE-backed runtime and control-plane updates.'],
      access: ['Access', 'API tokens and node certificates. Secrets are returned only once.'],
    } satisfies Record<Section, [string, string]>,
    empty: 'No data yet', loading: 'Loading control plane', logout: 'Sign out', refresh: 'Refresh', cancel: 'Cancel', save: 'Save', create: 'Create', remove: 'Remove', revoke: 'Revoke', enable: 'Enable', disable: 'Disable', details: 'Details', copied: 'Copied', copy: 'Copy',
  },
  ru: {
    brandSub: 'Панель управления', groups: ['Флот', 'Доставка', 'Аналитика', 'Система'],
    nav: { overview: 'Обзор', nodes: 'Ноды', subscriptions: 'Подписки', users: 'Пользователи', boards: 'Борды', traffic: 'Трафик', activity: 'События', access: 'Доступ' } satisfies Record<Section, string>,
    search: 'Поиск нод, пользователей, подписок', live: 'Онлайн', reconnecting: 'Переподключение', allNodes: 'Все ноды',
    titles: {
      overview: ['Обзор флота', 'Здоровье нод, desired-state drift и трафик в реальном времени.'],
      nodes: ['Ноды', 'Регистрация, расхождение desired/applied и runtime-факты по каждой ноде.'],
      subscriptions: ['Подписки', 'Стабильные идентификаторы доставки и привязки к нодам и пользователям.'],
      users: ['Пользователи', 'Учётные данные, лимиты и квоты. Приватные ключи доступны только на запись.'],
      boards: ['Борды', 'Хеши бордов, лимиты полос, runtime-состояние и размещение по нодам.'],
      traffic: ['Трафик', 'Счётчики интерфейса и пользовательский payload хранятся и запрашиваются раздельно.'],
      activity: ['События', 'Runtime-события и обновления control plane через защищённый SSE-поток.'],
      access: ['Доступ', 'API-токены и сертификаты нод. Секреты возвращаются только один раз.'],
    } satisfies Record<Section, [string, string]>,
    empty: 'Данных пока нет', loading: 'Загрузка control plane', logout: 'Выйти', refresh: 'Обновить', cancel: 'Отмена', save: 'Сохранить', create: 'Создать', remove: 'Удалить', revoke: 'Отозвать', enable: 'Включить', disable: 'Выключить', details: 'Подробнее', copied: 'Скопировано', copy: 'Копировать',
  },
} as const

export function t(language: Language) { return copy[language] }
