import * as React from "react";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { Download, Upload, Loader2, DatabaseBackup, AlertTriangle } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";

export function Maintenance() {
  const fileRef = React.useRef<HTMLInputElement>(null);
  const [pending, setPending] = React.useState<File | null>(null);

  const importMut = useMutation({
    mutationFn: (file: File) => api.importBackup(file),
    onSuccess: () => {
      toast.success("Бэкап загружен", {
        description: "Сервер перезапускается с восстановленной базой.",
      });
      setPending(null);
    },
    onError: (e: Error) => {
      toast.error("Не удалось импортировать", { description: e.message });
      setPending(null);
    },
  });

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Обслуживание</h1>
        <p className="text-sm text-muted-foreground">Резервное копирование базы данных</p>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <DatabaseBackup className="h-4 w-4 text-muted-foreground" />
            <CardTitle className="text-base">Экспорт</CardTitle>
          </div>
          <CardDescription>
            Скачать консистентный снимок базы (пользователи, доски, статистика) одним
            файлом .db.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button asChild variant="outline">
            <a href={api.backupURL()} download>
              <Download className="h-4 w-4" /> Скачать бэкап
            </a>
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Upload className="h-4 w-4 text-muted-foreground" />
            <CardTitle className="text-base">Импорт</CardTitle>
          </div>
          <CardDescription>
            Загрузить ранее сохранённый файл .db. Текущая база будет заменена, сервер
            перезапустится.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <input
            ref={fileRef}
            type="file"
            accept=".db,application/octet-stream,application/x-sqlite3"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) setPending(f);
              e.target.value = "";
            }}
          />
          <Button variant="outline" onClick={() => fileRef.current?.click()} disabled={importMut.isPending}>
            {importMut.isPending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Upload className="h-4 w-4" />
            )}
            Выбрать файл…
          </Button>
        </CardContent>
      </Card>

      <AlertDialog open={!!pending} onOpenChange={(v) => !v && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="flex items-center gap-2">
              <AlertTriangle className="h-5 w-5 text-destructive" />
              Заменить базу данных?
            </AlertDialogTitle>
            <AlertDialogDescription>
              Файл <span className="font-medium text-foreground">{pending?.name}</span> заменит
              текущую базу целиком. Все текущие клиенты, доски и статистика будут
              перезаписаны данными из бэкапа, а сервер перезапустится. Действие
              необратимо.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Отмена</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => pending && importMut.mutate(pending)}
            >
              Заменить и перезапустить
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
