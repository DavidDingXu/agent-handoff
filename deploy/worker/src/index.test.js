import assert from "node:assert/strict";
import test from "node:test";

import worker, { BudgetGate } from "./index.js";

const LIVE_BYTES_LIMIT = 512 * 1024 * 1024;

class MemoryStorage {
  constructor(entries = []) {
    this.data = new Map(entries);
  }

  async get(key) {
    return this.data.get(key);
  }

  async put(key, value) {
    this.data.set(key, value);
  }

  async delete(key) {
    return this.data.delete(key);
  }

  async list(options = {}) {
    const entries = [...this.data.entries()]
      .filter(([key]) => !options.prefix || key.startsWith(options.prefix))
      .filter(([key]) => !options.startAfter || key > options.startAfter)
      .sort(([a], [b]) => a.localeCompare(b))
      .slice(0, options.limit);
    return new Map(entries);
  }
}

class MemoryKV {
  constructor() {
    this.data = new Map();
    this.puts = [];
    this.deletes = [];
  }

  async put(key, value, options) {
    this.data.set(key, value);
    this.puts.push({ key, options });
  }

  async get(key) {
    return this.data.get(key) || null;
  }

  async delete(key) {
    this.data.delete(key);
    this.deletes.push(key);
  }
}

function currentMonth() {
  const d = new Date();
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, "0")}`;
}

function currentDay() {
  return new Date().toISOString().slice(0, 10);
}

function gateRequest(gate, path, body) {
  return gate.fetch(new Request(`https://do${path}`, {
    method: "POST",
    body: JSON.stringify(body),
  }));
}

function gateBinding(gate) {
  return {
    idFromName: (name) => name,
    get: () => ({
      fetch: (input, init) => gate.fetch(new Request(input, init)),
    }),
  };
}

function validManifest(ttlSeconds) {
  return {
    schema: "agent-handoff.link.v1",
    ttl_seconds: ttlSeconds,
    thread: { id: "thread-1", title: "Task" },
    bundle: { sha256: "a".repeat(64), bytes: 3 },
    crypto: {
      alg: "AES-256-GCM",
      nonce: "abcdefghijklmnop",
      key_ref: "url-fragment:k",
    },
  };
}

test("KV uploads clamp TTL and commit quota metadata", async () => {
  const storage = new MemoryStorage();
  const kv = new MemoryKV();
  const gate = new BudgetGate({ storage }, { SHARE_KV: kv });
  const env = {
    SHARE_KV: kv,
    SHARE_BUDGET_GATE: gateBinding(gate),
  };
  const manifest = JSON.stringify(validManifest(1));
  const form = new FormData();
  form.set("manifest", manifest);
  form.set("blob", new Blob([new Uint8Array([1, 2, 3])]), "blob.enc");

  const response = await worker.fetch(new Request("https://share.example/v1/shares", {
    method: "POST",
    body: form,
  }), env, {});

  assert.equal(response.status, 201);
  assert.equal(kv.puts.length, 1);
  assert.equal(kv.puts[0].options.expirationTtl, 60);
  const share = [...storage.data.entries()].find(([key]) => key.startsWith("share:"))[1];
  assert.equal(share.manifest.ttl_seconds, 60);
  assert.equal((await storage.get("budget")).liveBytes, 3 + manifest.length);
});

test("relay page is static and does not require storage bindings", async () => {
  const response = await worker.fetch(new Request("https://share.example/r"), {}, {});
  const body = await response.text();

  assert.equal(response.status, 200);
  assert.match(response.headers.get("content-type"), /^text\/html/);
  assert.equal(response.headers.get("referrer-policy"), "no-referrer");
  assert.match(body, /URLSearchParams\(location\.hash/);
  assert.match(body, /agent-handoff import/);
  assert.match(body, /#h=/);
  assert.match(body, /claude plugin install agent-handoff@agent-handoff/);
  assert.match(body, /codex plugin add agent-handoff@agent-handoff/);
  assert.match(body, /github\.com\/DavidDingXu\/agent-handoff/);
});

test("commit enforces the live byte limit even after preflight", async () => {
  const storage = new MemoryStorage([
    ["budget", { month: currentMonth(), liveBytes: LIVE_BYTES_LIMIT, puts: 1, gets: 0 }],
  ]);
  const gate = new BudgetGate({ storage }, { SHARE_KV: new MemoryKV() });

  const response = await gateRequest(gate, "/commit", {
    id: "over-limit",
    objectKey: "shares/over-limit/blob.enc",
    bytes: 1,
    expiresAt: new Date(Date.now() + 60_000).toISOString(),
    manifest: {},
  });

  assert.equal(response.status, 507);
  assert.equal((await response.json()).error, "live_bytes_limit");
  assert.equal(await storage.get("share:over-limit"), undefined);
  assert.equal((await storage.get("budget")).liveBytes, LIVE_BYTES_LIMIT);
});

test("month rollover preserves live bytes while resetting counters", async () => {
  const storage = new MemoryStorage([
    ["budget", { month: "2000-01", liveBytes: 100, puts: 99, gets: 88 }],
  ]);
  const gate = new BudgetGate({ storage }, { SHARE_KV: new MemoryKV() });

  const response = await gateRequest(gate, "/commit", {
    id: "new-month",
    objectKey: "shares/new-month/blob.enc",
    bytes: 25,
    expiresAt: new Date(Date.now() + 60_000).toISOString(),
    manifest: {},
  });

  assert.equal(response.status, 200);
  assert.deepEqual(await storage.get("budget"), {
    month: currentMonth(),
    day: currentDay(),
    liveBytes: 125,
    puts: 1,
    dailyPuts: 1,
    gets: 0,
  });
});

test("daily upload budget rejects new shares without consuming quota", async () => {
  const storage = new MemoryStorage([
    ["budget", { month: currentMonth(), day: currentDay(), liveBytes: 100, puts: 800, dailyPuts: 800, gets: 0 }],
  ]);
  const gate = new BudgetGate({ storage }, { SHARE_KV: new MemoryKV() });

  const response = await gateRequest(gate, "/reserve", { bytes: 1 });

  assert.equal(response.status, 429);
  assert.equal((await response.json()).error, "daily_put_limit");
  assert.equal((await storage.get("budget")).liveBytes, 100);
});

test("download counting enforces the per-share limit in the gate", async () => {
  const storage = new MemoryStorage([
    ["budget", { month: currentMonth(), liveBytes: 100, puts: 1, gets: 9 }],
    ["share:limited", { downloads: 10 }],
  ]);
  const gate = new BudgetGate({ storage }, { SHARE_KV: new MemoryKV() });

  const response = await gateRequest(gate, "/download?id=limited", {});

  assert.equal(response.status, 200);
  assert.equal((await response.json()).error, "download_limit");
  assert.equal((await storage.get("budget")).gets, 9);
  assert.equal((await storage.get("share:limited")).downloads, 10);
});

test("cleanup deletes expired blobs and releases live bytes", async () => {
  const kv = new MemoryKV();
  const expiredKey = "shares/expired/blob.enc";
  kv.data.set(expiredKey, new ArrayBuffer(3));
  const storage = new MemoryStorage([
    ["budget", { month: currentMonth(), liveBytes: 300, puts: 2, gets: 0 }],
    ["share:active", {
      objectKey: "shares/active/blob.enc",
      bytes: 200,
      expiresAt: new Date(Date.now() + 60_000).toISOString(),
    }],
    ["share:expired", {
      objectKey: expiredKey,
      bytes: 100,
      expiresAt: new Date(Date.now() - 60_000).toISOString(),
    }],
  ]);
  const gate = new BudgetGate({ storage }, { SHARE_KV: kv });

  const response = await gateRequest(gate, "/cleanup", {});
  const result = await response.json();

  assert.equal(response.status, 200);
  assert.deepEqual(result, { ok: true, removed: 1, released_bytes: 100 });
  assert.deepEqual(kv.deletes, [expiredKey]);
  assert.equal(await storage.get("share:expired"), undefined);
  assert.notEqual(await storage.get("share:active"), undefined);
  assert.equal((await storage.get("budget")).liveBytes, 200);
});
