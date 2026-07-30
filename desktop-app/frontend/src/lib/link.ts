/**
 * Lightweight synchronous parsing for bproxy:// links. Go performs the
 * authoritative validation before a connection is started.
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
  const parts = splitLink(link.trim());
  if (!parts || parts.boards.length === 0) return false;
  return base64urlLen(parts.token) === TOKEN_BYTES;
}
