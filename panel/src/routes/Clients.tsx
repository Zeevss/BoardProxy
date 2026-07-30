import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { QRCodeSVG } from "qrcode.react";
import { toast } from "sonner";
import {
  MoreHorizontal,
  Plus,
  Copy,
  Check,
  ChevronDown,
  ChevronRight,
  Loader2,
  UserPlus,
} from "lucide-react";
import { api, type ClientInfo, type CreateClientResponse } from "@/lib/api";
import { formatBytes, formatDate } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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
import { ClientConnections } from "@/components/ClientConnections";

export function Clients() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["clients"],
    queryFn: () => api.listClients(),
    refetchInterval: 5000,
  });

  const [createOpen, setCreateOpen] = React.useState(false);
  const [keylink, setKeylink] = React.useState<CreateClientResponse | null>(null);
  const [renaming, setRenaming] = React.useState<ClientInfo | null>(null);
  const [deleting, setDeleting] = React.useState<ClientInfo | null>(null);
  const [expanded, setExpanded] = React.useState<number | null>(null);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["clients"] });

  return (
    <div className="mx-auto max-w-5xl space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Клиенты</h1>
          <p className="text-sm text-muted-foreground">Пользователи с доступом к прокси</p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>
          <Plus className="h-4 w-4" /> Новый клиент
        </Button>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-8" />
              <TableHead>ID</TableHead>
              <TableHead>Имя</TableHead>
              <TableHead>Статус</TableHead>
              <TableHead>Трафик (вх/исх)</TableHead>
              <TableHead>Создан</TableHead>
              <TableHead>Был на связи</TableHead>
              <TableHead className="w-8" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <SkeletonRows cols={8} />
            ) : !data?.length ? (
              <TableRow>
                <TableCell colSpan={8} className="py-10 text-center text-muted-foreground">
                  Клиентов пока нет
                </TableCell>
              </TableRow>
            ) : (
              data.map((c) => (
                <React.Fragment key={c.id}>
                  <TableRow>
                    <TableCell>
                      <button
                        onClick={() => setExpanded(expanded === c.id ? null : c.id)}
                        className="text-muted-foreground hover:text-foreground"
                        aria-label="Показать соединения"
                      >
                        {expanded === c.id ? (
                          <ChevronDown className="h-4 w-4" />
                        ) : (
                          <ChevronRight className="h-4 w-4" />
                        )}
                      </button>
                    </TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">{c.id}</TableCell>
                    <TableCell className="font-medium">{c.name}</TableCell>
                    <TableCell>
                      {c.status === "active" ? (
                        <Badge variant="success">активен</Badge>
                      ) : (
                        <Badge variant="muted">отключён</Badge>
                      )}
                    </TableCell>
                    <TableCell className="tabular-nums text-muted-foreground">
                      {formatBytes(c.rx_bytes)} / {formatBytes(c.tx_bytes)}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{formatDate(c.created_at)}</TableCell>
                    <TableCell className="text-muted-foreground">{formatDate(c.last_seen)}</TableCell>
                    <TableCell>
                      <RowMenu
                        client={c}
                        onRename={() => setRenaming(c)}
                        onDelete={() => setDeleting(c)}
                        onChanged={invalidate}
                      />
                    </TableCell>
                  </TableRow>
                  {expanded === c.id && (
                    <TableRow className="hover:bg-transparent">
                      <TableCell colSpan={8} className="bg-muted/30">
                        <ClientConnections clientId={c.id} />
                      </TableCell>
                    </TableRow>
                  )}
                </React.Fragment>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      <CreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(resp) => {
          setKeylink(resp);
          invalidate();
        }}
      />
      <KeylinkDialog data={keylink} onClose={() => setKeylink(null)} />
      <RenameDialog client={renaming} onClose={() => setRenaming(null)} onDone={invalidate} />
      <DeleteDialog client={deleting} onClose={() => setDeleting(null)} onDone={invalidate} />
    </div>
  );
}

function RowMenu({
  client,
  onRename,
  onDelete,
  onChanged,
}: {
  client: ClientInfo;
  onRename: () => void;
  onDelete: () => void;
  onChanged: () => void;
}) {
  const toggle = useMutation({
    mutationFn: () =>
      api.updateClient(client.id, {
        status: client.status === "active" ? "disabled" : "active",
      }),
    onSuccess: onChanged,
    onError: (e: Error) => toast.error("Ошибка", { description: e.message }),
  });

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="h-8 w-8">
          <MoreHorizontal className="h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={onRename}>Переименовать</DropdownMenuItem>
        <DropdownMenuItem onClick={() => toggle.mutate()}>
          {client.status === "active" ? "Отключить" : "Включить"}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className="text-destructive focus:text-destructive"
          onClick={onDelete}
        >
          Удалить навсегда
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function DeleteDialog({
  client,
  onClose,
  onDone,
}: {
  client: ClientInfo | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const remove = useMutation({
    mutationFn: () => api.deleteClient(client!.id),
    onSuccess: () => {
      toast.success(`Клиент «${client!.name}» удалён`);
      onClose();
      onDone();
    },
    onError: (e: Error) => toast.error("Ошибка", { description: e.message }),
  });

  return (
    <AlertDialog open={!!client} onOpenChange={(v) => !v && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Удалить клиента «{client?.name}»?</AlertDialogTitle>
          <AlertDialogDescription>
            Клиент и его ключ будут удалены из базы навсегда, активные сессии
            разорваны. Ранее выданный keylink перестанет работать. Действие
            необратимо. Чтобы временно закрыть доступ без удаления — используйте
            «Отключить».
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Отмена</AlertDialogCancel>
          <AlertDialogAction
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            onClick={(e) => {
              e.preventDefault();
              remove.mutate();
            }}
          >
            Удалить
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function CreateDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: (resp: CreateClientResponse) => void;
}) {
  const [name, setName] = React.useState("");
  const create = useMutation({
    mutationFn: () => api.createClient(name.trim()),
    onSuccess: (resp) => {
      onOpenChange(false);
      setName("");
      onCreated(resp);
    },
    onError: (e: Error) => toast.error("Не удалось создать", { description: e.message }),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Новый клиент</DialogTitle>
          <DialogDescription>
            Сервер сгенерирует ключевую пару и выдаст keylink — строку подключения.
            Она показывается один раз.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) create.mutate();
          }}
          className="space-y-4"
        >
          <div className="space-y-2">
            <Label htmlFor="client-name">Имя</Label>
            <Input
              id="client-name"
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="например, alice-laptop"
            />
          </div>
          <DialogFooter>
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              {create.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <UserPlus className="h-4 w-4" />
              )}
              Создать
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function KeylinkDialog({
  data,
  onClose,
}: {
  data: CreateClientResponse | null;
  onClose: () => void;
}) {
  const [copied, setCopied] = React.useState(false);
  React.useEffect(() => {
    if (data) setCopied(false);
  }, [data]);

  return (
    <Dialog open={!!data} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Клиент «{data?.name}» создан</DialogTitle>
          <DialogDescription>
            Передайте keylink клиенту или отсканируйте QR-код в приложении. Ключ
            больше не будет показан.
          </DialogDescription>
        </DialogHeader>
        {data && (
          <div className="space-y-4">
            <div className="flex justify-center rounded-lg bg-white p-4">
              <QRCodeSVG value={data.keylink} size={200} level="M" />
            </div>
            <div className="space-y-2">
              <Label>Keylink</Label>
              <div className="flex gap-2">
                <Input readOnly value={data.keylink} className="font-mono text-xs" />
                <Button
                  variant="outline"
                  size="icon"
                  onClick={async () => {
                    await navigator.clipboard.writeText(data.keylink);
                    setCopied(true);
                    toast.success("Keylink скопирован");
                  }}
                >
                  {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                </Button>
              </div>
            </div>
          </div>
        )}
        <DialogFooter>
          <Button onClick={onClose}>Готово</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RenameDialog({
  client,
  onClose,
  onDone,
}: {
  client: ClientInfo | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const [name, setName] = React.useState("");
  React.useEffect(() => {
    if (client) setName(client.name);
  }, [client]);

  const rename = useMutation({
    mutationFn: () => api.updateClient(client!.id, { name: name.trim() }),
    onSuccess: () => {
      onClose();
      onDone();
    },
    onError: (e: Error) => toast.error("Ошибка", { description: e.message }),
  });

  return (
    <Dialog open={!!client} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Переименовать клиента</DialogTitle>
        </DialogHeader>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) rename.mutate();
          }}
          className="space-y-4"
        >
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} />
          <DialogFooter>
            <Button type="submit" disabled={!name.trim() || rename.isPending}>
              Сохранить
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function SkeletonRows({ cols }: { cols: number }) {
  return (
    <>
      {Array.from({ length: 4 }).map((_, i) => (
        <TableRow key={i}>
          {Array.from({ length: cols }).map((__, j) => (
            <TableCell key={j}>
              <Skeleton className="h-4 w-full" />
            </TableCell>
          ))}
        </TableRow>
      ))}
    </>
  );
}
