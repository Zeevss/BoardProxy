import type { SubscriptionProfileSnapshot } from "@/types";

/**
 * Lightweight synchronous parsing for direct bproxy:// links and subscription
 * URLs. Go and Subscribe SDK perform authoritative validation before saving or
 * connecting.
 */

const PREFIX = "bproxy://";
const TOKEN_BYTES = 64;

interface LinkParts {
  token: string;
  boards: string[];
  label: string;
}

export function splitLink(link: string): LinkParts | null {
  if (!link.startsWith(PREFIX)) return null;
  let rest = link.slice(PREFIX.length);

  let label = "";
  const hash = rest.indexOf("#");
  if (hash >= 0) {
    try {
      label = decodeURIComponent(rest.slice(hash + 1));
    } catch {
      return null;
    }
    rest = rest.slice(0, hash);
  }

  const at = rest.lastIndexOf("@");
  if (at < 0) return null;
  const boards = rest
    .slice(at + 1)
    .split(",")
    .map((board) => board.trim())
    .filter(Boolean);
  return { token: rest.slice(0, at), boards, label };
}

export function linkBoards(link: string): string {
  return splitLink(link)?.boards.join(", ") ?? "";
}

export function linkLabel(link: string): string {
  return splitLink(link)?.label ?? "";
}

export function isSubscriptionLink(link: string): boolean {
  try {
    const value = new URL(link.trim());
    const parts = value.pathname.split("/").filter(Boolean);
    return (
      (value.protocol === "https:" || value.protocol === "http:") &&
      parts.length >= 2 &&
      parts[parts.length - 2] === "s" &&
      !!parts[parts.length - 1] &&
      value.hash.startsWith("#bp1=")
    );
  } catch {
    return false;
  }
}

export function linkSummary(link: string): string {
  if (isSubscriptionLink(link)) return "Подписка · автоматическое обновление";
  return linkBoards(link) || "ключ не задан";
}

export function subscriptionSnapshotFromInfo(info: {
  kind: string;
  subscriptionId?: string;
  revision?: string;
  keys?: Array<{
    id: string;
    name: string;
    nodeId: string;
    state: string;
    usedBytes: number;
    boards: string[];
  }>;
}): SubscriptionProfileSnapshot | undefined {
  if (info.kind !== "subscription") return undefined;
  return {
    id: info.subscriptionId ?? "",
    revision: info.revision ?? "",
    keys: (info.keys ?? []).map((key) => ({
      id: key.id,
      name: key.name,
      nodeId: key.nodeId,
      state: key.state,
      usedBytes: key.usedBytes,
      boards: key.boards ?? [],
    })),
  };
}

function base64urlLen(token: string): number | null {
  try {
    let b64 = token.replace(/-/g, "+").replace(/_/g, "/");
    const pad = b64.length % 4;
    if (pad) b64 += "=".repeat(4 - pad);
    return atob(b64).length;
  } catch {
    return null;
  }
}

export function isValidLink(link: string): boolean {
  if (isSubscriptionLink(link)) return true;
  const parts = splitLink(link.trim());
  if (!parts || parts.boards.length === 0) return false;
  return base64urlLen(parts.token) === TOKEN_BYTES;
}
