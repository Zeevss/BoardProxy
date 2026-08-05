import { useState } from "react";
import { ChartSpline, Save } from "lucide-react";
import {
  Button,
  Card,
  CardHeader,
  CardTitle,
  Chip,
  Field,
  Input,
} from "@/components/ui";
import { useSettingsStore } from "@/store/settings";
import { isValidPort, isValidRegex } from "@/lib/utils";

export function ProxySettings({ onOpenDebug }: { onOpenDebug: () => void }) {
  const port = useSettingsStore((state) => state.port);
  const listenAddr = useSettingsStore((state) => state.listenAddr);
  const bypassList = useSettingsStore((state) => state.bypassList);
  const setPort = useSettingsStore((state) => state.setPort);
  const setListenAddr = useSettingsStore((state) => state.setListenAddr);
  const addBypass = useSettingsStore((state) => state.addBypass);
  const removeBypass = useSettingsStore((state) => state.removeBypass);

  const [draftPort, setDraftPort] = useState(String(port));
  const [draftAddr, setDraftAddr] = useState(listenAddr);
  const [saved, setSaved] = useState(false);
  const [bypassInput, setBypassInput] = useState("");

  const saveListenAddress = () => {
    const nextPort = Number(draftPort);
	if (!isValidPort(nextPort)) return;
    setPort(nextPort);
    setListenAddr(draftAddr.trim() || "127.0.0.1");
    setSaved(true);
    window.setTimeout(() => setSaved(false), 1500);
  };

  const bypassTrimmed = bypassInput.trim();
  const bypassValid = bypassTrimmed === "" || isValidRegex(bypassTrimmed);
  const addPattern = () => {
    if (!bypassTrimmed || !bypassValid) return;
    addBypass(bypassTrimmed);
    setBypassInput("");
  };

  return (
    <div className="mx-auto max-w-3xl space-y-5">
      <Card>
        <CardHeader>
          <CardTitle>Локальный прокси</CardTitle>
        </CardHeader>
		<div className="grid grid-cols-1 gap-3 min-[760px]:grid-cols-3">
          <Field label="Адрес" className="min-[760px]:col-span-2">
            <Input
              value={draftAddr}
              onChange={(event) => setDraftAddr(event.target.value)}
              placeholder="127.0.0.1"
            />
          </Field>
          <Field label="Порт">
            <Input
              type="number"
              min={1}
              max={65535}
              value={draftPort}
              onChange={(event) => setDraftPort(event.target.value)}
            />
          </Field>
		</div>
		<p className="mt-3 text-xs text-muted">
		  Максимальное число lanes задаёт сервер доски и передаёт клиенту при рукопожатии.
		</p>

        <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
          <div className="min-w-0 break-all font-mono text-xs text-muted">
            socks5://{draftAddr || "127.0.0.1"}:{draftPort}
          </div>
          <Button variant="primary" size="sm" onClick={saveListenAddress}>
            {saved && <Save size={14} />}
            {saved ? "Сохранено" : "Применить"}
          </Button>
        </div>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Диагностика транспорта</CardTitle>
        </CardHeader>
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-3 text-muted">
            <ChartSpline size={17} className="shrink-0 text-accent" />
            <span className="text-xs">Графики окон, RTT, очередей и lanes.</span>
          </div>
          <Button variant="secondary" size="sm" onClick={onOpenDebug} className="shrink-0">
            Открыть
          </Button>
        </div>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Обход туннеля</CardTitle>
        </CardHeader>
        <p className="mb-3 text-xs text-muted">
          Хосты по этим regexp идут напрямую, мимо туннеля.
        </p>

        {bypassList.length === 0 ? (
          <p className="py-4 text-center text-sm text-muted/70">
            Пусто — весь трафик через BoardProxy.
          </p>
        ) : (
          <div className="flex flex-wrap gap-2">
            {bypassList.map((pattern) => (
              <Chip key={pattern} onRemove={() => removeBypass(pattern)}>
                <span className="font-mono">{pattern}</span>
              </Chip>
            ))}
          </div>
        )}

        <div className="mt-4 flex gap-2">
          <Input
            placeholder="^example\\.com$ или \\.local$"
            value={bypassInput}
            onChange={(event) => setBypassInput(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                addPattern();
              }
            }}
            className="flex-1 font-mono"
          />
          <Button
            variant="secondary"
            size="sm"
            onClick={addPattern}
            disabled={!bypassTrimmed || !bypassValid}
          >
            Добавить
          </Button>
        </div>
        {!bypassValid && (
          <p className="mt-2 text-xs text-danger">
            Некорректное регулярное выражение
          </p>
        )}
      </Card>
    </div>
  );
}
