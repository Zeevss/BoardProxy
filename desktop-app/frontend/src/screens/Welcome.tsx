import { useState } from "react";
import { ClipboardPaste, KeyRound, Pencil } from "lucide-react";
import { Button, Textarea } from "@/components/ui";
import { backend } from "@/lib/backend";
import { subscriptionSnapshotFromInfo } from "@/lib/link";
import { useProfilesStore } from "@/store/profiles";

/**
 * Приветственный экран при отсутствии профилей. Предлагает в один клик вставить
 * ссылку подписки или прямой ключ из буфера обмена,
 * с ручным вводом как запасным вариантом.
 */
export function Welcome() {
  const createProfile = useProfilesStore((s) => s.createProfile);
  const setActive = useProfilesStore((s) => s.setActive);

  const [manual, setManual] = useState(false);
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const addKey = async (raw: string) => {
    const key = raw.trim();
    if (!key) {
      setError("Ключ пуст.");
      setManual(true);
      return;
    }
    setBusy(true);
    setError("");
    try {
      const info = await backend.parseLink(key);
      const p = createProfile({
        name: info.label?.trim() || "Моя подписка",
        key,
        subscription: subscriptionSnapshotFromInfo(info),
      });
      setActive(p.id);
      // profiles.length станет > 0 — App переключится на главный экран.
    } catch (e) {
      setError(
        "Не похоже на подписку BoardProxy или прямой ключ. " +
          (e instanceof Error ? e.message : "")
      );
      setManual(true);
    } finally {
      setBusy(false);
    }
  };

  const fromClipboard = async () => {
    let text = "";
    try {
      text = await navigator.clipboard.readText();
    } catch {
      setError("Не удалось прочитать буфер обмена. Вставьте ключ вручную.");
      setManual(true);
      return;
    }
    await addKey(text);
  };

  return (
    <div className="flex h-full w-full items-center justify-center p-6">
      <div className="w-full max-w-md text-center">
        <div className="mx-auto mb-5 flex h-16 w-16 items-center justify-center rounded-2xl border border-accent/30 bg-accent/10 text-accent">
          <KeyRound size={30} />
        </div>
        <h1 className="text-xl font-semibold text-fg">Добро пожаловать в BoardProxy</h1>
        <p className="mt-1.5 text-sm text-muted">
          Вставьте ссылку подписки или прямой ключ bproxy://, чтобы начать.
        </p>

        <div className="mt-6 space-y-3">
          <Button
            variant="primary"
            className="w-full justify-center"
            onClick={fromClipboard}
            disabled={busy}
          >
            <ClipboardPaste size={16} />
            Вставить из буфера обмена
          </Button>

          {!manual ? (
            <button
              type="button"
              onClick={() => setManual(true)}
              className="inline-flex items-center gap-1.5 text-xs text-muted hover:text-fg"
            >
              <Pencil size={13} /> Ввести вручную
            </button>
          ) : (
            <div className="space-y-2 text-left">
              <Textarea
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder="https://subscribe.example.com/s/…#bp1=…"
                rows={3}
                className="font-mono text-xs"
              />
              <Button
                variant="secondary"
                className="w-full justify-center"
                onClick={() => addKey(value)}
                disabled={busy || !value.trim()}
              >
                Добавить профиль
              </Button>
            </div>
          )}

          {error && <p className="text-xs text-danger">{error}</p>}
        </div>
      </div>
    </div>
  );
}
