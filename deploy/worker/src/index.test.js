import assert from "node:assert/strict";
import test from "node:test";

import worker, { BudgetGate } from "./index.js";

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

test("KV uploads use configured TTL and keep only share metadata", async () => {
  const storage = new MemoryStorage();
  const kv = new MemoryKV();
  const gate = new BudgetGate({ storage }, { SHARE_KV: kv });
  const env = {
    SHARE_KV: kv,
    SHARE_BUDGET_GATE: gateBinding(gate),
    SHARE_DEFAULT_TTL_SECONDS: "90",
    SHARE_MAX_TTL_SECONDS: "120",
    SHARE_MAX_BLOB_BYTES: "10",
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
  assert.equal(await storage.get("budget"), undefined);
  assert.equal(share.downloads, undefined);
  assert.equal(share.manifest.service, undefined);
});

test("capabilities expose configured retention and size without project quotas", async () => {
  const kv = new MemoryKV();
  const response = await worker.fetch(new Request("https://share.example/v1/capabilities"), {
    SHARE_KV: kv,
    SHARE_DEFAULT_TTL_SECONDS: "1800",
    SHARE_MAX_TTL_SECONDS: "7200",
    SHARE_MAX_BLOB_BYTES: "1048576",
  }, {});
  const capabilities = await response.json();

  assert.equal(response.status, 200);
  assert.equal(capabilities.schema, "agent-handoff.link-service.v1");
  assert.equal(capabilities.default_ttl_seconds, 1800);
  assert.equal(capabilities.max_ttl_seconds, 7200);
  assert.equal(capabilities.max_blob_bytes, 1048576);
  for (const removed of [
    "max_downloads_per_share",
    "max_live_bytes",
    "daily_upload_limit",
    "monthly_upload_limit",
    "quota_policy",
  ]) {
    assert.equal(removed in capabilities, false, `${removed} should not be advertised`);
  }
});

test("configured blob size limit rejects oversized ciphertext", async () => {
  const storage = new MemoryStorage();
  const kv = new MemoryKV();
  const gate = new BudgetGate({ storage }, { SHARE_KV: kv });
  const manifest = JSON.stringify(validManifest(60));
  const form = new FormData();
  form.set("manifest", manifest);
  form.set("blob", new Blob([new Uint8Array([1, 2, 3])]), "blob.enc");

  const response = await worker.fetch(new Request("https://share.example/v1/shares", {
    method: "POST",
    body: form,
  }), {
    SHARE_KV: kv,
    SHARE_BUDGET_GATE: gateBinding(gate),
    SHARE_MAX_BLOB_BYTES: "2",
  }, {});

  assert.equal(response.status, 413);
  assert.deepEqual(await response.json(), { error: "blob_too_large", max_bytes: 2 });
  assert.equal(kv.puts.length, 0);
  assert.equal([...storage.data.keys()].some((key) => key.startsWith("share:")), false);
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

test("legacy budget state no longer blocks new shares", async () => {
  const legacyBudget = { liveBytes: Number.MAX_SAFE_INTEGER, puts: Number.MAX_SAFE_INTEGER, gets: Number.MAX_SAFE_INTEGER };
  const storage = new MemoryStorage([
    ["budget", legacyBudget],
  ]);
  const gate = new BudgetGate({ storage }, { SHARE_KV: new MemoryKV() });

  const response = await gateRequest(gate, "/commit", {
    id: "new-share",
    objectKey: "shares/new-share/blob.enc",
    bytes: 1,
    expiresAt: new Date(Date.now() + 60_000).toISOString(),
    manifest: {},
  });

  assert.equal(response.status, 200);
  assert.deepEqual(await storage.get("budget"), legacyBudget);
  assert.notEqual(await storage.get("share:new-share"), undefined);
});

test("downloads are not counted or limited by the project", async () => {
  const kv = new MemoryKV();
  const objectKey = "shares/reusable/blob.enc";
  kv.data.set(objectKey, new Uint8Array([1, 2, 3]).buffer);
  const storage = new MemoryStorage([
    ["share:reusable", {
      id: "reusable",
      objectKey,
      downloads: 10,
      expiresAt: new Date(Date.now() + 60_000).toISOString(),
      manifest: {},
    }],
  ]);
  const gate = new BudgetGate({ storage }, { SHARE_KV: kv });
  const env = { SHARE_KV: kv, SHARE_BUDGET_GATE: gateBinding(gate) };

  const first = await worker.fetch(new Request("https://share.example/v1/shares/reusable/blob"), env, {});
  const second = await worker.fetch(new Request("https://share.example/v1/shares/reusable/blob"), env, {});

  assert.equal(first.status, 200);
  assert.equal(second.status, 200);
  assert.deepEqual(new Uint8Array(await second.arrayBuffer()), new Uint8Array([1, 2, 3]));
  assert.equal((await storage.get("share:reusable")).downloads, 10);
});

test("cleanup deletes expired blobs without touching legacy budget state", async () => {
  const kv = new MemoryKV();
  const expiredKey = "shares/expired/blob.enc";
  kv.data.set(expiredKey, new ArrayBuffer(3));
  const storage = new MemoryStorage([
    ["budget", { month: "legacy", liveBytes: 300, puts: 2, gets: 0 }],
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
  assert.deepEqual(result, { ok: true, removed: 1 });
  assert.deepEqual(kv.deletes, [expiredKey]);
  assert.equal(await storage.get("share:expired"), undefined);
  assert.notEqual(await storage.get("share:active"), undefined);
  assert.equal((await storage.get("budget")).liveBytes, 300);
});
