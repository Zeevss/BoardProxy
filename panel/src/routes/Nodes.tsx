import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { Loader2, Plus, Server, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, type NodeInfo } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";

export function Nodes() {
  const [adding, setAdding] = React.useState(false);
  const { data = [], isLoading } = useQuery({ queryKey: ["nodes"], queryFn: api.listNodes });

  return (
    <div className="mx-auto max-w-5xl space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Ноды</h1>
          <p className="text-sm text-muted-foreground">
            Выберите BoardProxy-сервер или добавьте удалённую ноду по ключу доступа.
          </p>
        </div>
        <Button onClick={() => setAdding(true)}><Plus className="h-4 w-4" /> Добавить</Button>
      </div>

      {isLoading ? (
        <Card className="p-8 text-center text-muted-foreground">Загрузка…</Card>
      ) : data.length === 0 ? (
        <Card className="flex flex-col items-center gap-3 p-10 text-center">
          <Server className="h-9 w-9 text-muted-foreground" />
          <div className="font-medium">Нет подключённых нод</div>
          <p className="max-w-md text-sm text-muted-foreground">
            На сервере выполните <code>./bproxy serve keygen panel</code>, затем добавьте полученный ключ сюда.
          </p>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {data.map((node) => <NodeCard key={node.id} node={node} />)}
        </div>
      )}
      <AddNodeDialog open={adding} onOpenChange={setAdding} />
    </div>
  );
}

function NodeCard({ node }: { node: NodeInfo }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const status = useQuery({
    queryKey: ["node-status", node.id],
    queryFn: () => api.nodeStatus(node.id),
    refetchInterval: 10_000,
  });
  const select = useMutation({
    mutationFn: () => api.selectNode(node.id),
    onSuccess: async () => {
      await qc.invalidateQueries();
      navigate("/");
    },
    onError: (e: Error) => toast.error("Не удалось выбрать ноду", { description: e.message }),
  });
  const remove = useMutation({
    mutationFn: () => api.deleteNode(node.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["nodes"] }),
    onError: (e: Error) => toast.error("Не удалось удалить ноду", { description: e.message }),
  });
  const online = status.data?.online === true;

  return (
    <Card className={node.selected ? "border-primary/60 p-5" : "p-5"}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Server className="h-4 w-4 text-primary" />
            <span className="truncate font-semibold">{node.name}</span>
            {node.selected && <Badge>выбрана</Badge>}
          </div>
          <div className="mt-2 font-mono text-xs text-muted-foreground">
            {node.tls ? "https" : "http"}://{node.host}:{node.port}
          </div>
          <div className="mt-1 text-xs text-muted-foreground">ключ {node.key_hint}</div>
        </div>
        <Badge variant={online ? "success" : "muted"}>
          {status.isLoading ? "проверка" : online ? `online · ${status.data?.latency_ms} ms` : "offline"}
        </Badge>
      </div>
      {status.data?.error && <p className="mt-3 text-xs text-destructive">{status.data.error}</p>}
      <div className="mt-4 flex justify-between gap-2">
        <Button variant="outline" size="sm" onClick={() => select.mutate()} disabled={select.isPending}>
          {node.selected ? "Открыть" : "Выбрать"}
        </Button>
        <Button variant="ghost" size="icon" onClick={() => remove.mutate()} disabled={remove.isPending}>
          <Trash2 className="h-4 w-4 text-destructive" />
        </Button>
      </div>
    </Card>
  );
}

function AddNodeDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = React.useState("");
  const [host, setHost] = React.useState("");
  const [port, setPort] = React.useState("8080");
  const [accessKey, setAccessKey] = React.useState("");
  const [tls, setTLS] = React.useState(false);
  const create = useMutation({
    mutationFn: () => api.createNode({ name: name.trim(), host: host.trim(), port: Number(port), tls, access_key: accessKey.trim() }),
    onSuccess: async () => {
      onOpenChange(false);
      await qc.invalidateQueries();
      navigate("/");
    },
    onError: (e: Error) => toast.error("Не удалось добавить ноду", { description: e.message }),
  });
  const valid = name.trim() && host.trim() && Number(port) > 0 && Number(port) <= 65535 && accessKey.trim().startsWith("bpa_");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Добавить ноду</DialogTitle>
          <DialogDescription>Ключ создаётся локально на ноде через <code>./bproxy serve keygen panel</code>.</DialogDescription>
        </DialogHeader>
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (valid) create.mutate(); }}>
          <div className="space-y-2"><Label>Название</Label><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} /></div>
          <div className="grid grid-cols-[1fr_8rem] gap-3">
            <div className="space-y-2"><Label>IP или hostname</Label><Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="10.0.0.10" /></div>
            <div className="space-y-2"><Label>Порт</Label><Input type="number" min={1} max={65535} value={port} onChange={(e) => setPort(e.target.value)} /></div>
          </div>
          <div className="space-y-2"><Label>Ключ доступа</Label><Input type="password" value={accessKey} onChange={(e) => setAccessKey(e.target.value)} placeholder="bpa_…" /></div>
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={tls} onChange={(e) => setTLS(e.target.checked)} /> HTTPS/TLS</label>
          <DialogFooter><Button type="submit" disabled={!valid || create.isPending}>{create.isPending && <Loader2 className="h-4 w-4 animate-spin" />} Добавить и открыть</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
