// Package store — порт хранилища идентичности и обслуживаемых хабов:
// предоставленные оператором пользователи и доски (хабы), которые сервер
// обслуживает. Пакет не знает о конкретной СУБД — реализация лежит в дочернем
// пакете sqlite; верхние слои зависят только от интерфейса Store.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound возвращается, когда запрошенная запись отсутствует.
var ErrNotFound = errors.New("store: not found")

// ErrConflict возвращается при нарушении уникальности — повторном public_key
// пользователя (см. Store.CreateUser).
var ErrConflict = errors.New("store: conflict")

// UserStatus — состояние пользователя.
type UserStatus string

const (
	UserActive   UserStatus = "active"
	UserDisabled UserStatus = "disabled"
)

// User — пользователь, предоставленный оператором заранее: self-service
// регистрации нет, доступ имеют только те, чей public_key уже в хранилище.
type User struct {
	ID        int64
	PublicKey []byte
	Name      string
	Status    UserStatus
	CreatedAt time.Time
	LastSeen  time.Time // нулевое значение — ни разу не подключался
	// RxBytes — байт, полученных сервером ОТ этого клиента (его исходящий
	// трафик/upload), TxBytes — байт, отправленных клиенту (его входящий
	// трафик/download). Копится за все ЗАВЕРШЁННЫЕ сессии (см.
	// Store.AddUserTraffic); трафик активной сессии сюда попадает только
	// после её закрытия — «на лету» его даёт hub.Server (см. mgmt ClientInfo,
	// который суммирует персистентное с живым).
	RxBytes uint64
	TxBytes uint64
}

// HubStatus — состояние хаба.
type HubStatus string

const (
	HubActive   HubStatus = "active"
	HubDisabled HubStatus = "disabled"
)

// Hub — доска, которую обслуживает сервер (1:1: один хаб — одна доска, id —
// её хэш).
type Hub struct {
	ID        string
	Name      string
	HubSlide  string
	Status    HubStatus
	CreatedAt time.Time
}

// Store — порт хранилища. Боевая реализация — store/sqlite.
type Store interface {
	// CreateUser заводит нового предоставленного оператором пользователя.
	// Возвращает ErrConflict, если public_key уже занят.
	CreateUser(ctx context.Context, pubKey []byte, name string) (User, error)
	// UserByPublicKey ищет пользователя по идентити-ключу. ErrNotFound
	// здесь и есть отказ в авторизации.
	UserByPublicKey(ctx context.Context, pubKey []byte) (User, error)
	// UserByID ищет пользователя по id (для управления). ErrNotFound, если нет.
	UserByID(ctx context.Context, id int64) (User, error)
	// ListUsers возвращает всех пользователей (для управления).
	ListUsers(ctx context.Context) ([]User, error)
	// SetUserStatus меняет статус пользователя (active/disabled). Отключённый
	// пользователь не проходит авторизацию на хабе — это и есть «rm».
	SetUserStatus(ctx context.Context, id int64, status UserStatus) error
	// SetUserName переименовывает пользователя (для управления).
	SetUserName(ctx context.Context, id int64, name string) error
	// AddUserTraffic ДОБАВЛЯЕТ rx/tx к накопленному трафику пользователя
	// (не заменяет) — вызывается hub'ом при закрытии клиентской сессии с её
	// финальными байтами. ErrNotFound, если пользователя уже нет.
	AddUserTraffic(ctx context.Context, id int64, rx, tx uint64) error
	// TouchUser отмечает last_seen пользователя текущим временем — вызывается
	// hub'ом при успешной авторизации подключающегося клиента. ErrNotFound, если
	// пользователя уже нет.
	TouchUser(ctx context.Context, id int64) error
	// DeleteUser безвозвратно удаляет пользователя (в отличие от
	// SetUserStatus(disabled), который лишь отзывает доступ). ErrNotFound, если
	// такого нет.
	DeleteUser(ctx context.Context, id int64) error

	// UpsertHub заводит или обновляет запись хаба (id — хэш доски).
	// Идемпотентно: вызывается при каждом старте сервера на этой доске.
	UpsertHub(ctx context.Context, id, name, hubSlide string) (Hub, error)
	// ListHubs возвращает все известные хабы (для управления).
	ListHubs(ctx context.Context) ([]Hub, error)
	// SetHubStatus меняет статус хаба (active/disabled).
	SetHubStatus(ctx context.Context, id string, status HubStatus) error
	// SetHubName переименовывает хаб (для управления), не трогая hub_slide.
	SetHubName(ctx context.Context, id string, name string) error
	// DeleteHub безвозвратно удаляет запись хаба (в отличие от
	// SetHubStatus(disabled)). ErrNotFound, если такого нет.
	DeleteHub(ctx context.Context, id string) error

	// Close освобождает ресурсы хранилища.
	Close() error
}
