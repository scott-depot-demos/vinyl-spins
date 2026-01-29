// In production we typically run UI and API on the same origin behind an Ingress.
// In that case, leaving VITE_API_URL unset makes requests same-origin.
const API_URL = (import.meta.env.VITE_API_URL as string | undefined) ?? (import.meta.env.PROD ? "" : "http://localhost:8080");

function qs(params: Record<string, string | undefined>): string {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v && v.trim()) u.set(k, v);
  }
  const s = u.toString();
  return s ? `?${s}` : "";
}

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_URL}${path}`, {
    credentials: "include",
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers || {}),
    },
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(`${path} failed: ${res.status}${text ? `: ${text}` : ""}`);
  }
  // 204: no content
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  async healthz(): Promise<string> {
    const res = await fetch(`${API_URL}/healthz`);
    if (!res.ok) throw new Error(`healthz failed: ${res.status}`);
    return await res.text();
  },

  async me(): Promise<{ user_id: string; discogs_user_id: number; discogs_username: string }> {
    return await fetchJSON("/api/me");
  },

  async albums(input?: {
    q?: string;
    artist?: string;
    label_ids?: string; // comma-separated
    sort?: "artist" | "title" | "spin_count" | "last_spun_at";
    order?: "asc" | "desc";
  }): Promise<
    Array<{
      id: string;
      discogs_release_id: number;
      title: string;
      artist: string;
      year?: number;
      thumb_url?: string;
      resource_url?: string;
      last_synced_at?: string;
      spin_count: number;
      last_spun_at?: string;
      labels: Array<{ id: string; name: string }>;
    }>
  > {
    return await fetchJSON(
      `/api/albums${qs({
        q: input?.q,
        artist: input?.artist,
        label_ids: input?.label_ids,
        sort: input?.sort,
        order: input?.order,
      })}`,
    );
  },

  async syncAlbums(): Promise<{ status: string }> {
    return await fetchJSON("/api/albums/sync", { method: "POST", body: "{}" });
  },

  async labels(): Promise<Array<{ id: string; name: string; album_count: number }>> {
    return await fetchJSON("/api/labels");
  },

  async createLabel(input: { name: string }): Promise<{ id: string; name: string }> {
    return await fetchJSON("/api/labels", { method: "POST", body: JSON.stringify(input) });
  },

  async addAlbumLabel(albumID: string, input: { label_id?: string; name?: string }): Promise<void> {
    await fetchJSON(`/api/albums/${encodeURIComponent(albumID)}/labels`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },

  async removeAlbumLabel(albumID: string, labelID: string): Promise<void> {
    await fetchJSON(`/api/albums/${encodeURIComponent(albumID)}/labels/${encodeURIComponent(labelID)}`, {
      method: "DELETE",
    });
  },

  async spins(): Promise<
    Array<{
      id: string;
      album_id: string;
      spun_at: string;
      note?: string;
      album_title: string;
      album_artist: string;
      album_thumb_url?: string;
    }>
  > {
    return await fetchJSON("/api/spins");
  },

  async createSpin(input: { album_id: string; spun_at?: string; note?: string }): Promise<{ id: string }> {
    return await fetchJSON("/api/spins", { method: "POST", body: JSON.stringify(input) });
  },

  async deleteSpin(spinID: string): Promise<void> {
    await fetchJSON(`/api/spins/${encodeURIComponent(spinID)}`, { method: "DELETE" });
  },

  async logout(): Promise<void> {
    await fetchJSON("/auth/logout", { method: "POST", body: "{}" });
  },
};

