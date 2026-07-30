import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { MoreHorizontal, Plus, Loader2 } from "lucide-react";
import { api, type BoardInfo } from "@/lib/api";
import { formatDate } from "@/lib/format";
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

export function Boards() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["boards"],
    queryFn: () => api.listBoards(),
    refetchInterval: 8000,
  });
  const [createOpen, setCreateOpen] = React.useState(false);
  const [renaming, setRenaming] = React.useState<BoardInfo | null>(null);
  const [deleting, setDeleting] = React.useState<BoardInfo | null>(null);
  const invalidate = () => qc.invalidateQueries({ queryKey: ["boards"] });

  return (
    <div className="mx-auto max-w-5xl space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Доски</h1>
          <p className="text-sm text-muted-foreground">
            Доски-хабы, которые обслуживает сервер (после добавления новой нужен
            перезапуск)
          </p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>
          <Plus className="h-4 w-4" /> Добавить доску
        </Button>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Хэш</TableHead>
              <TableHead>Имя</TableHead>
              <TableHead>Hub-слайд</TableHead>
              <TableHead>Статус</TableHead>
              <TableHead>Добавлена</TableHead>
              <TableHead className="w-8" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <SkeletonRows cols={6} />
            ) : !data?.length ? (
              <TableRow>
                <TableCell colSpan={6} className="py-10 text-center text-muted-foreground">
                  Досок пока нет
                </TableCell>
              </TableRow>
            ) : (
              data.map((b) => (
                <TableRow key={b.id}>
                  <TableCell className="max-w-[16rem] truncate font-mono text-xs">{b.id}</TableCell>
                  <TableCell className="font-medium">{b.name || "—"}</TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {b.hub_slide || "—"}
                  </TableCell>
                  <TableCell>
                    {b.status === "active" ? (
                      <Badge variant="success">активна</Badge>
                    ) : (
                      <Badge variant="muted">отключена</Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground">{formatDate(b.created_at)}</TableCell>
                  <TableCell>
                    <RowMenu
                      board={b}
                      onRename={() => setRenaming(b)}
                      onDelete={() => setDeleting(b)}
                      onChanged={invalidate}
                    />
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      <CreateDialog open={createOpen} onOpenChange={setCreateOpen} onDone={invalidate} />
      <RenameDialog board={renaming} onClose={() => setRenaming(null)} onDone={invalidate} />
      <DeleteDialog board={deleting} onClose={() => setDeleting(null)} onDone={invalidate} />
    </div>
  );
}

function RowMenu({
  board,
  onRename,
  onDelete,
  onChanged,
}: {
  board: BoardInfo;
  onRename: () => void;
  onDelete: () => void;
  onChanged: () => void;
}) {
  const toggle = useMutation({
    mutationFn: () =>
      api.updateBoard(board.id, { status: board.status === "active" ? "disabled" : "active" }),
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
          {board.status === "active" ? "Отключить" : "Включить"}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem className="text-destructive focus:text-destructive" onClick={onDelete}>
          Удалить навсегда
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function DeleteDialog({
  board,
  onClose,
  onDone,
}: {
  board: BoardInfo | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const remove = useMutation({
    mutationFn: () => api.deleteBoard(board!.id),
    onSuccess: () => {
      toast.success("Доска удалена");
      onClose();
      onDone();
    },
    onError: (e: Error) => toast.error("Ошибка", { description: e.message }),
  });

  return (
    <AlertDialog open={!!board} onOpenChange={(v) => !v && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Удалить доску?</AlertDialogTitle>
          <AlertDialogDescription>
            Запись доски <span className="font-mono text-foreground">{board?.name || board?.id}</span>{" "}
            будет удалена из базы навсегда. Если сервер сейчас её обслуживает, хаб
            остановится при следующем перезапуске. Чтобы временно снять с
            обслуживания без удаления — используйте «Отключить».
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
  onDone,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onDone: () => void;
}) {
  const [id, setId] = React.useState("");
  const [name, setName] = React.useState("");
  const create = useMutation({
    mutationFn: () => api.createBoard(id.trim(), name.trim()),
    onSuccess: () => {
      onOpenChange(false);
      setId("");
      setName("");
      onDone();
      toast.success("Доска добавлена", {
        description: "Перезапустите сервер, чтобы он начал её обслуживать.",
      });
    },
    onError: (e: Error) => toast.error("Не удалось добавить", { description: e.message }),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Новая доска</DialogTitle>
          <DialogDescription>
            Укажите хэш доски (whiteboard hash). Сервер начнёт её обслуживать после
            перезапуска.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (id.trim()) create.mutate();
          }}
          className="space-y-4"
        >
          <div className="space-y-2">
            <Label htmlFor="board-id">Хэш доски</Label>
            <Input
              id="board-id"
              autoFocus
              value={id}
              onChange={(e) => setId(e.target.value)}
              className="font-mono text-xs"
              placeholder="1272cae57eef80dda58036f3ac627c2b"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="board-name">Имя (необязательно)</Label>
            <Input id="board-name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <DialogFooter>
            <Button type="submit" disabled={!id.trim() || create.isPending}>
              {create.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Добавить
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RenameDialog({
  board,
  onClose,
  onDone,
}: {
  board: BoardInfo | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const [name, setName] = React.useState("");
  React.useEffect(() => {
    if (board) setName(board.name);
  }, [board]);

  const rename = useMutation({
    mutationFn: () => api.updateBoard(board!.id, { name: name.trim() }),
    onSuccess: () => {
      onClose();
      onDone();
    },
    onError: (e: Error) => toast.error("Ошибка", { description: e.message }),
  });

  return (
    <Dialog open={!!board} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Переименовать доску</DialogTitle>
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
      {Array.from({ length: 3 }).map((_, i) => (
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
