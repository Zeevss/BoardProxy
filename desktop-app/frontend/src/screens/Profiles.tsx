import { useState } from "react";
import { Plus, Pencil, Trash2, Server, Check } from "lucide-react";
import { Button, Card, Modal, Field, Input, Textarea } from "@/components/ui";
import { useProfilesStore, type ProfileInput } from "@/store/profiles";
import { linkBoards, isValidLink } from "@/lib/link";

export function Profiles() {
  const profiles = useProfilesStore((s) => s.profiles);
  const activeId = useProfilesStore((s) => s.activeId);
  const setActive = useProfilesStore((s) => s.setActive);
  const createProfile = useProfilesStore((s) => s.createProfile);
  const updateProfile = useProfilesStore((s) => s.updateProfile);
  const deleteProfile = useProfilesStore((s) => s.deleteProfile);

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

  return (
    <div className="mx-auto max-w-4xl space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted">
          {profiles.length}{" "}
          {profiles.length === 1 ? "профиль" : profiles.length < 5 ? "профиля" : "профилей"}
        </p>
        <Button size="sm" onClick={openAdd}>
          <Plus size={15} /> Добавить
        </Button>
      </div>

      {profiles.length === 0 ? (
        <EmptyState onAdd={openAdd} />
      ) : (
        <div className="grid gap-3 min-[760px]:grid-cols-2">
          {profiles.map((p) => (
            <ProfileCard
              key={p.id}
              profile={p}
              active={p.id === activeId}
              onSelect={() => setActive(p.id)}
              onEdit={() => openEdit(p.id)}
              onDelete={() => handleDelete(p.id)}
            />
          ))}
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
            <span className="truncate">{linkBoards(profile.key) || "ключ не задан"}</span>
          </div>
          {profile.note && (
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

  const keyTrimmed = key.trim();
  const keyValid = keyTrimmed === "" || isValidLink(keyTrimmed);
  const boards = keyValid ? linkBoards(keyTrimmed) : "";
  const canSave = !!name.trim() && isValidLink(keyTrimmed);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSave) return;
    onSave({
      name: name.trim(),
      key: keyTrimmed,
      note: note.trim() || undefined,
    });
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
        label="Ключ"
        hint="Ссылка подключения bproxy://…"
      >
        <Textarea
          rows={3}
          placeholder="bproxy://<token>@<board>#label"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          className="font-mono text-xs"
        />
      </Field>
      {keyTrimmed !== "" && !keyValid && (
        <p className="-mt-2 text-xs text-danger">
          Неверная ссылка bproxy:// (проверьте токен и доску)
        </p>
      )}
      {boards && (
        <p className="-mt-2 font-mono text-xs text-muted">Доски: {boards}</p>
      )}
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
        <Button type="submit" variant="primary" disabled={!canSave}>
          {initial ? "Сохранить" : "Создать"}
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
