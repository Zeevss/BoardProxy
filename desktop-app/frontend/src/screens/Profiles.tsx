import { useEffect, useState } from "react";
import {
  Plus,
  Pencil,
  Trash2,
  Server,
  Check,
  FolderTree,
  KeyRound,
  RefreshCw,
} from "lucide-react";
import { Button, Card, Modal, Field, Input, Textarea } from "@/components/ui";
import { useProfilesStore, type ProfileInput } from "@/store/profiles";
import {
  isSubscriptionLink,
  isValidLink,
  linkBoards,
  linkSummary,
  subscriptionSnapshotFromInfo,
} from "@/lib/link";
import { backend } from "@/lib/backend";
import { formatBytes } from "@/lib/utils";
import type { Profile, SubscriptionProfileKey } from "@/types";

export function Profiles() {
  const profiles = useProfilesStore((s) => s.profiles);
  const activeId = useProfilesStore((s) => s.activeId);
  const setActive = useProfilesStore((s) => s.setActive);
  const createProfile = useProfilesStore((s) => s.createProfile);
  const updateProfile = useProfilesStore((s) => s.updateProfile);
  const deleteProfile = useProfilesStore((s) => s.deleteProfile);
  const [refreshingIds, setRefreshingIds] = useState<Set<string>>(
    () => new Set(),
  );
  const [refreshErrors, setRefreshErrors] = useState<Record<string, string>>({});

  const subscriptionProfiles = profiles.filter((profile) =>
    isSubscriptionLink(profile.key),
  );
  const standaloneProfiles = profiles.filter(
    (profile) => !isSubscriptionLink(profile.key),
  );
  const missingSnapshotSignature = subscriptionProfiles
    .filter((profile) => !profile.subscription)
    .map((profile) => `${profile.id}:${profile.key}`)
    .join("|");

  // Обновляем только старые профили без метаданных и делаем запросы параллельно.
  useEffect(() => {
    if (!missingSnapshotSignature) return;
    const missing = useProfilesStore
      .getState()
      .profiles.filter(
        (profile) => isSubscriptionLink(profile.key) && !profile.subscription,
      );
    let cancelled = false;
    setRefreshingIds((current) => addIds(current, missing.map((profile) => profile.id)));

    void Promise.all(
      missing.map(async (profile) => {
        try {
          const info = await backend.parseLink(profile.key);
          const subscription = subscriptionSnapshotFromInfo(info);
          if (!subscription) throw new Error("Ссылка больше не является подпиской");
          if (!cancelled) updateProfile(profile.id, { subscription });
        } catch (cause) {
          if (!cancelled) {
            setRefreshErrors((current) => ({
              ...current,
              [profile.id]: cause instanceof Error ? cause.message : String(cause),
            }));
          }
        } finally {
          if (!cancelled) {
            setRefreshingIds((current) => removeId(current, profile.id));
          }
        }
      }),
    );

    return () => {
      cancelled = true;
    };
  }, [missingSnapshotSignature, updateProfile]);

  const [modal, setModal] = useState<{
    open: boolean;
    mode: "add" | "edit";
    id: string | null;
  }>({ open: false, mode: "add", id: null });

  const openAdd = () => setModal({ open: true, mode: "add", id: null });
  const openEdit = (id: string) => setModal({ open: true, mode: "edit", id });

  const handleSave = (input: ProfileInput) => {
    if (modal.mode === "add") {
      const p = createProfile(input);
      setActive(p.id);
    } else if (modal.id) {
      updateProfile(modal.id, input);
    }
    setModal({ open: false, mode: "add", id: null });
  };

  const handleDelete = (id: string) => {
    deleteProfile(id);
    if (modal.id === id) setModal({ open: false, mode: "add", id: null });
  };

  const refreshSubscription = async (profile: Profile) => {
    setRefreshingIds((current) => addIds(current, [profile.id]));
    setRefreshErrors((current) => ({ ...current, [profile.id]: "" }));
    try {
      const info = await backend.parseLink(profile.key);
      const subscription = subscriptionSnapshotFromInfo(info);
      if (!subscription) throw new Error("Ссылка больше не является подпиской");
      updateProfile(profile.id, { subscription });
    } catch (cause) {
      setRefreshErrors((current) => ({
        ...current,
        [profile.id]: cause instanceof Error ? cause.message : String(cause),
      }));
    } finally {
      setRefreshingIds((current) => removeId(current, profile.id));
    }
  };

  return (
    <div className="mx-auto max-w-4xl space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted">
          {subscriptionProfiles.length} {plural(subscriptionProfiles.length, "подписка", "подписки", "подписок")}
          {" · "}
          {standaloneProfiles.length} {plural(standaloneProfiles.length, "отдельный ключ", "отдельных ключа", "отдельных ключей")}
        </p>
        <Button size="sm" onClick={openAdd}>
          <Plus size={15} /> Добавить
        </Button>
      </div>

      {profiles.length === 0 ? (
        <EmptyState onAdd={openAdd} />
      ) : (
        <div className="space-y-6">
          {subscriptionProfiles.length > 0 && (
            <section className="space-y-3" aria-labelledby="subscriptions-heading">
              <GroupHeading
                id="subscriptions-heading"
                icon={<FolderTree size={16} />}
                title="Подписки"
                count={subscriptionProfiles.length}
              />
              {subscriptionProfiles.map((profile) => (
                <SubscriptionGroup
                  key={profile.id}
                  profile={profile}
                  active={profile.id === activeId}
                  refreshing={refreshingIds.has(profile.id)}
                  error={refreshErrors[profile.id]}
                  onSelect={() => setActive(profile.id)}
                  onRefresh={() => void refreshSubscription(profile)}
                  onEdit={() => openEdit(profile.id)}
                  onDelete={() => handleDelete(profile.id)}
                />
              ))}
            </section>
          )}

          {standaloneProfiles.length > 0 && (
            <section className="space-y-3" aria-labelledby="standalone-heading">
              <GroupHeading
                id="standalone-heading"
                icon={<KeyRound size={16} />}
                title="Отдельные ключи"
                count={standaloneProfiles.length}
              />
              <div className="grid gap-3 min-[760px]:grid-cols-2">
                {standaloneProfiles.map((profile) => (
                  <ProfileCard
                    key={profile.id}
                    profile={profile}
                    active={profile.id === activeId}
                    onSelect={() => setActive(profile.id)}
                    onEdit={() => openEdit(profile.id)}
                    onDelete={() => handleDelete(profile.id)}
                  />
                ))}
              </div>
            </section>
          )}
        </div>
      )}

      <Modal
        open={modal.open}
        onClose={() => setModal({ open: false, mode: "add", id: null })}
        title={modal.mode === "add" ? "Новый профиль" : "Редактировать профиль"}
      >
        <ProfileForm
          key={modal.id ?? "new"}
          initial={
            modal.mode === "edit" && modal.id
              ? profiles.find((p) => p.id === modal.id)
              : undefined
          }
          onSave={handleSave}
          onCancel={() => setModal({ open: false, mode: "add", id: null })}
        />
      </Modal>
    </div>
  );
}

function GroupHeading({
  id,
  icon,
  title,
  count,
}: {
  id: string;
  icon: React.ReactNode;
  title: string;
  count: number;
}) {
  return (
    <div className="flex items-center gap-2 text-sm font-medium text-fg">
      <span className="text-muted">{icon}</span>
      <h2 id={id}>{title}</h2>
      <span className="rounded-md bg-surface-2 px-1.5 py-0.5 text-[11px] text-muted">
        {count}
      </span>
    </div>
  );
}

function SubscriptionGroup({
  profile,
  active,
  refreshing,
  error,
  onSelect,
  onRefresh,
  onEdit,
  onDelete,
}: {
  profile: Profile;
  active: boolean;
  refreshing: boolean;
  error?: string;
  onSelect: () => void;
  onRefresh: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const keys = profile.subscription?.keys ?? [];
  return (
    <Card className={active ? "ring-2 ring-accent/60 bg-accent/5" : ""}>
      <div
        className="flex cursor-pointer items-start justify-between gap-3"
        onClick={onSelect}
      >
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate text-sm font-medium text-fg">{profile.name}</span>
            {active && (
              <span className="flex items-center gap-1 rounded-md bg-accent/15 px-1.5 py-0.5 text-[10px] font-medium text-accent">
                <Check size={10} /> активно
              </span>
            )}
          </div>
          <p className="mt-1 text-xs text-muted">
            {keys.length} {plural(keys.length, "ключ", "ключа", "ключей")}
          </p>
          {profile.note && <p className="mt-1.5 text-xs text-muted/80">{profile.note}</p>}
        </div>
        <div className="flex shrink-0 gap-1" onClick={(event) => event.stopPropagation()}>
          <Button
            variant="ghost"
            size="icon"
            onClick={onRefresh}
            disabled={refreshing}
            aria-label={`Обновить подписку ${profile.name}`}
          >
            <RefreshCw size={15} className={refreshing ? "animate-spin" : ""} />
          </Button>
          <Button variant="ghost" size="icon" onClick={onEdit} aria-label="Редактировать">
            <Pencil size={15} />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={onDelete}
            aria-label="Удалить"
            className="text-muted hover:text-danger"
          >
            <Trash2 size={15} />
          </Button>
        </div>
      </div>

      <div className="mt-4">
        {refreshing && keys.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-border bg-surface-2 px-4 py-6 text-center text-xs text-muted">
            Загружаем ключи подписки…
          </div>
        ) : keys.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-border bg-surface-2 px-4 py-6 text-center text-xs text-muted">
            В подписке пока нет ключей
          </div>
        ) : (
          <div className="grid gap-3 min-[760px]:grid-cols-2">
            {keys.map((key) => <SubscriptionKeyCard key={key.id} item={key} />)}
          </div>
        )}
        {error && <p className="mt-2 text-xs text-danger">Не удалось обновить: {error}</p>}
      </div>
    </Card>
  );
}

function SubscriptionKeyCard({ item }: { item: SubscriptionProfileKey }) {
  const enabled = item.state === "enabled";
  return (
    <article
      className={`card p-5 transition-all duration-150 hover:shadow-soft ${
        enabled ? "" : "opacity-60"
      }`}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-medium text-fg">
              {item.name || item.id}
            </span>
            <span className={`shrink-0 text-xs ${enabled ? "text-accent" : "text-muted"}`}>
              {enabled ? "Включён" : "Отключён"}
            </span>
          </div>
          <div className="mt-1.5 flex min-w-0 items-center gap-1.5 font-mono text-xs text-muted">
            <Server size={12} className="shrink-0" />
            <span className="truncate">
              {item.boards.length > 0 ? item.boards.join(", ") : "доска не указана"}
            </span>
          </div>
          <p className="mt-2 truncate text-xs text-muted/80">
            {item.nodeId || "Нода не указана"} · {formatBytes(item.usedBytes)} использовано
          </p>
        </div>
      </div>
    </article>
  );
}

/* ---------- Профиль-карточка ---------- */

function ProfileCard({
  profile,
  active,
  onSelect,
  onEdit,
  onDelete,
}: {
  profile: import("@/types").Profile;
  active: boolean;
  onSelect: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <Card
      className={`cursor-pointer transition-all duration-150 hover:shadow-soft ${
        active ? "ring-2 ring-accent/60 bg-accent/5" : ""
      }`}
      onClick={onSelect}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-fg truncate">
              {profile.name}
            </span>
            {active && (
              <span className="flex items-center gap-1 rounded-md bg-accent/15 px-1.5 py-0.5 text-[10px] font-medium text-accent">
                <Check size={10} /> активно
              </span>
            )}
          </div>
          <div className="mt-1.5 flex min-w-0 items-center gap-1.5 font-mono text-xs text-muted">
            <Server size={12} className="shrink-0" />
            <span className="truncate">{linkSummary(profile.key)}</span>
          </div>
          {visibleProfileNote(profile.note) && (
            <p className="mt-2 text-xs text-muted/80 line-clamp-2">{profile.note}</p>
          )}
        </div>
        <div
          className="flex shrink-0 gap-1"
          onClick={(e) => e.stopPropagation()}
        >
          <Button
            variant="ghost"
            size="icon"
            onClick={onEdit}
            aria-label="Редактировать"
          >
            <Pencil size={15} />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={onDelete}
            aria-label="Удалить"
            className="text-muted hover:text-danger"
          >
            <Trash2 size={15} />
          </Button>
        </div>
      </div>
    </Card>
  );
}

/* ---------- Форма профиля ---------- */

function ProfileForm({
  initial,
  onSave,
  onCancel,
}: {
  initial?: import("@/types").Profile;
  onSave: (input: ProfileInput) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(initial?.name ?? "");
  const [key, setKey] = useState(initial?.key ?? "");
  const [note, setNote] = useState(initial?.note ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const keyTrimmed = key.trim();
  const keyValid = keyTrimmed === "" || isValidLink(keyTrimmed);
  const boards = keyValid ? linkBoards(keyTrimmed) : "";
  const canSave = !!name.trim() && isValidLink(keyTrimmed);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSave) return;
    setBusy(true);
    setError("");
    try {
      const info = await backend.parseLink(keyTrimmed);
      onSave({
        name: name.trim(),
        key: keyTrimmed,
        note: note.trim() || undefined,
        subscription: subscriptionSnapshotFromInfo(info),
      });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      <Field label="Название">
        <Input
          placeholder="Amsterdam #1"
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
        />
      </Field>
      <Field
        label="Ссылка подключения"
        hint="Подписка https://… или прямой ключ bproxy://…"
      >
        <Textarea
          rows={3}
          placeholder="https://subscribe.example.com/s/…#bp1=…"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          className="font-mono text-xs"
        />
      </Field>
      {keyTrimmed !== "" && !keyValid && (
        <p className="-mt-2 text-xs text-danger">
          Неверная ссылка подписки или bproxy:// ключ
        </p>
      )}
      {isSubscriptionLink(keyTrimmed) && keyValid && (
        <p className="-mt-2 text-xs text-accent">
          Подписка будет обновляться перед каждым подключением; клиент выберет
          первый включённый ключ.
        </p>
      )}
      {boards && (
        <p className="-mt-2 font-mono text-xs text-muted">Доски: {boards}</p>
      )}
      {error && <p className="-mt-2 text-xs text-danger">{error}</p>}
      <Field label="Заметка" hint="Необязательно">
        <Textarea
          rows={2}
          placeholder="Нидерланды · низкая задержка"
          value={note}
          onChange={(e) => setNote(e.target.value)}
        />
      </Field>

      <div className="flex justify-end gap-2 pt-2">
        <Button variant="ghost" type="button" onClick={onCancel}>
          Отмена
        </Button>
        <Button type="submit" variant="primary" disabled={!canSave || busy}>
          {busy ? "Проверяем…" : initial ? "Сохранить" : "Создать"}
        </Button>
      </div>
    </form>
  );
}

/* ---------- Пустое состояние ---------- */

function EmptyState({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center rounded-2xl border-2 border-dashed border-border py-16">
      <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-2xl bg-surface-2 text-muted">
        <Server size={24} />
      </div>
      <p className="text-sm text-muted">Нет ни одного профиля</p>
      <Button variant="secondary" size="sm" className="mt-4" onClick={onAdd}>
        <Plus size={15} /> Создать первый
      </Button>
    </div>
  );
}

function addIds(current: Set<string>, ids: string[]): Set<string> {
  const next = new Set(current);
  for (const id of ids) next.add(id);
  return next;
}

function removeId(current: Set<string>, id: string): Set<string> {
  const next = new Set(current);
  next.delete(id);
  return next;
}

function plural(count: number, one: string, few: string, many: string): string {
  const mod100 = count % 100;
  if (mod100 >= 11 && mod100 <= 14) return many;
  const mod10 = count % 10;
  if (mod10 === 1) return one;
  if (mod10 >= 2 && mod10 <= 4) return few;
  return many;
}

function visibleProfileNote(note?: string): boolean {
  return !!note?.trim() && note.trim().toLocaleLowerCase("ru-RU") !== "добавлено вручную";
}
