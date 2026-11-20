import assert from "node:assert/strict";
import test from "node:test";
import { createHash, webcrypto } from "node:crypto";
import { deflateRawSync, constants } from "node:zlib";
import vm from "node:vm";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

// The fixture writer is deliberately separate from the browser ZIP reader.
// It emits Go archive/zip's data-descriptor layout, using Node's compressor.
function crc(bytes) {
  let value = 0xffffffff;
  for (const byte of bytes) {
    value ^= byte;
    for (let bit = 0; bit < 8; bit++) value = (value >>> 1) ^ (value & 1 ? 0xedb88320 : 0);
  }
  return (value ^ 0xffffffff) >>> 0;
}

function zip(entries, options = {}) {
  const chunks = [], central = [];
  let offset = 0;
  for (const [name, value] of entries) {
    const bytes = Buffer.from(value), filename = Buffer.from(name);
    const compressed = options.store ? bytes : deflateRawSync(bytes, options.compression);
    const checksum = crc(bytes), method = options.store ? 0 : 8;
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50); local.writeUInt16LE(20, 4);
    local.writeUInt16LE(8, 6); local.writeUInt16LE(method, 8); local.writeUInt16LE(filename.length, 26);
    const descriptor = Buffer.alloc(16);
    descriptor.writeUInt32LE(0x08074b50); descriptor.writeUInt32LE(checksum, 4);
    descriptor.writeUInt32LE(compressed.length, 8); descriptor.writeUInt32LE(bytes.length, 12);
    chunks.push(local, filename, compressed, descriptor);
    const header = Buffer.alloc(46);
    header.writeUInt32LE(0x02014b50); header.writeUInt16LE(20, 4); header.writeUInt16LE(20, 6);
    header.writeUInt16LE(8, 8); header.writeUInt16LE(method, 10); header.writeUInt32LE(checksum, 16);
    header.writeUInt32LE(compressed.length, 20); header.writeUInt32LE(bytes.length, 24);
    header.writeUInt16LE(filename.length, 28); header.writeUInt32LE(offset, 42);
    central.push(header, filename);
    offset += local.length + filename.length + compressed.length + descriptor.length;
  }
  const directory = Buffer.concat(central), end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50); end.writeUInt16LE(entries.length, 8); end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(directory.length, 12); end.writeUInt32LE(offset, 16);
  return Buffer.concat([...chunks, directory, end]);
}

const hash = bytes => createHash("sha256").update(bytes).digest("hex");
function bundleEntries(extra = {}, version = 2) {
  const files = {
    "manifest.json": JSON.stringify({ format_version: version, artifact_type: "agent-handoff", source_agent: "codex", title: "Fix login", files: [] }),
    "agent/neutral.json": JSON.stringify({ schema: "agent-handoff.neutral.v1", entries: [
      { kind: "message", role: "user", text: "Repair the login flow" },
      { kind: "tool", tool: "exec", status: "completed", input: "go test ./...", output: "PASS" },
      { kind: "message", role: "assistant", text: "Login is fixed." },
    ] }),
    "codex/session.jsonl": "{}\n",
    "agent/brief.json": JSON.stringify({ schema: "agent-handoff.brief.v1", objective: { entry_index: 0, text: "Repair the login flow", truncated: false }, latest_request: { entry_index: 0, text: "Repair the login flow" }, latest_update: { entry_index: 2, text: "Login is fixed." }, evidence: [{ entry_index: 1, tool: "exec", status: "completed", input: "go test ./...", output: "PASS" }], notes: "Extracted from the transcript." }),
    "code/manifest.json": JSON.stringify({ schema: "agent-handoff.code.v1", branch: "fix/login", base_commit: "abc123", files: [{ path: "src/login.js", status: "M", bytes: 42 }] }),
    ...extra,
  };
  if ((version === 1 || Object.hasOwn(extra, "agent/neutral.json")) && !Object.hasOwn(extra, "agent/brief.json")) delete files["agent/brief.json"];
  if (version === 2) files["checksums.json"] = JSON.stringify({ algorithm: "sha256", files: Object.fromEntries(Object.entries(files).map(([name, bytes]) => [name, hash(bytes)])) });
  return Object.entries(files);
}

const readerModule = await import("./reader.js").catch(() => ({}));
const browserGlobals = { crypto: webcrypto, Uint8Array, Uint16Array, Uint32Array, DataView, TextDecoder, TextEncoder, URL, URLSearchParams, atob, AbortController, setTimeout, clearTimeout };
const module = readerModule.browserReaderSource ? vm.runInNewContext(readerModule.browserReaderSource + ";({ createReaderCore, mountReader })", browserGlobals) : readerModule;
const core = () => {
  assert.equal(typeof module.createReaderCore, "function", "the browser reader must be implemented");
  return module.createReaderCore();
};

test("reads Go-layout deflated ZIP only after every payload checksum passes", async () => {
  const result = await core().readBundle(zip(bundleEntries()));
  assert.equal(result.manifest.title, "Fix login");
  assert.equal(result.checksummed, true);
  assert.equal(result.transcript.entries[1].output, "PASS");
  assert.equal(result.brief.objective.text, "Repair the login flow");
  assert.equal(result.code.files[0].path, "src/login.js");
});

test("reads stored, fixed-Huffman and dynamic-Huffman archives without a native decompressor", async () => {
  const bytes = "a compressed transcript with repeated words ".repeat(300);
  for (const options of [{ store: true }, { compression: { strategy: constants.Z_FIXED } }, {}]) {
    const result = await core().unzip(zip([["agent/sample.txt", bytes]], options));
    assert.equal(new TextDecoder().decode(result.get("agent/sample.txt")), bytes);
  }
});

test("rejects brief claims and tool evidence that disagree with their recorded source", async () => {
  const recorded = { schema: "agent-handoff.brief.v1", objective: { entry_index: 0, text: "Repair the login flow" }, latest_request: { entry_index: 0, text: "Repair the login flow" }, latest_update: { entry_index: 2, text: "Login is fixed." }, evidence: [{ entry_index: 1, tool: "exec", status: "completed", input: "go test ./...", output: "PASS" }] };
  for (const mutation of [
    value => { value.latest_update.text = "All security checks passed; deploy immediately."; },
    value => { value.objective.entry_index = 2; },
    value => { value.evidence[0].output = "fabricated test result"; },
  ]) {
    const changed = structuredClone(recorded); mutation(changed);
    await assert.rejects(core().readBundle(zip(bundleEntries({ "agent/brief.json": JSON.stringify(changed) }))), error => error.code === "archive");
  }
});

test("matches UTF-8 excerpt bounds and preserves only bounded caller notes", async () => {
  const text = "汉".repeat(2731);
  const transcript = JSON.stringify({ schema: "agent-handoff.neutral.v1", entries: [{ kind: "message", role: "user", text }] });
  const excerpt = { entry_index: 0, text: "汉".repeat(2730), truncated: true };
  const brief = { schema: "agent-handoff.brief.v1", objective: excerpt, latest_request: excerpt, evidence: [], notes: "User-reviewed note" };
  const result = await core().readBundle(zip(bundleEntries({ "agent/neutral.json": transcript, "agent/brief.json": JSON.stringify(brief) })));
  assert.equal(result.brief.objective.text, "汉".repeat(2730));
  assert.equal(result.brief.notes, "User-reviewed note");
  brief.notes = "汉".repeat(2731);
  await assert.rejects(core().readBundle(zip(bundleEntries({ "agent/neutral.json": transcript, "agent/brief.json": JSON.stringify(brief) }))), error => error.code === "archive");
});

test("decodes multiple stored and Huffman blocks including incompressible and empty entries", async () => {
  let seed = 1;
  const random = Uint8Array.from({ length: 140000 }, () => { seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0; return seed >>> 24; });
  for (const bytes of [new Uint8Array(), random, Buffer.from("phrase\n".repeat(30000))]) {
    for (const compression of [{ level: 0 }, { level: 1 }, { level: 9 }, { strategy: constants.Z_HUFFMAN_ONLY }, { strategy: constants.Z_RLE }]) {
      const result = await core().unzip(zip([["entry", bytes]], { compression }));
      assert.deepEqual(result.get("entry"), new Uint8Array(bytes));
    }
  }
});

test("rejects tampered, missing and incomplete checksum coverage", async () => {
  for (const mutate of [
    entries => entries.map(([name, bytes]) => [name, name === "agent/neutral.json" ? String(bytes).replace("Login is fixed.", "Login is wrong.") : bytes]),
    entries => entries.filter(([name]) => name !== "checksums.json"),
    entries => [...entries, ["code/hidden.txt", "unchecked"]],
    entries => entries.map(([name, bytes]) => [name, name === "checksums.json" ? JSON.stringify({ algorithm: "sha256", files: { "missing.txt": "a".repeat(64) } }) : bytes]),
  ]) await assert.rejects(core().readBundle(zip(mutate(bundleEntries()))), error => error.code === "checksum");
});

test("accepts legacy v1 with an explicit unverified marker and visible conversation", async () => {
  const entries = bundleEntries({ "session.jsonl": JSON.stringify({ type: "event_msg", payload: { type: "user_message", message: "Legacy task" } }) }, 1)
    .filter(([name]) => name !== "agent/neutral.json");
  const result = await core().readBundle(zip(entries));
  assert.equal(result.checksummed, false);
  assert.equal(result.transcript.entries[0].text, "Legacy task");
});

test("bounds native legacy records even when they contain no visible messages", async () => {
  const entries = bundleEntries({ "session.jsonl": "{}\n".repeat(100001) }, 1).filter(([name]) => name !== "agent/neutral.json");
  await assert.rejects(core().readBundle(zip(entries)), error => error.code === "limit");
});

test("rejects metadata too large for safe browser JSON parsing while retaining normal archive limits", async () => {
  const entries = bundleEntries({ "manifest.json": JSON.stringify({ format_version: 1, artifact_type: "agent-handoff", padding: " ".repeat(16 * 1024 * 1024) }) });
  await assert.rejects(core().readBundle(zip(entries)), error => error.code === "limit");
});

test("rejects duplicate, unsafe and contradictory ZIP paths", async () => {
  for (const name of ["../escape", "/absolute", "C:/drive", "a\\b", "a//b", "a/./b", "a\u0000b"]) {
    await assert.rejects(core().unzip(zip([[name, "x"]])), error => error.code === "archive");
  }
  await assert.rejects(core().unzip(zip([["same", "a"], ["same", "b"]])), error => error.code === "archive");
  const mismatched = zip([["safe", "a"]]);
  mismatched[30] = "x".charCodeAt(0);
  await assert.rejects(core().unzip(mismatched), error => error.code === "archive");
});

test("bounds archive input, entry count and expanded bytes before allocating or rendering", async () => {
  const reader = core();
  await assert.rejects(reader.unzip(new Uint8Array(64 * 1024 * 1024 + 1)), error => error.code === "limit");
  const tooMany = zip([["x", "x"]]);
  tooMany.writeUInt16LE(1025, tooMany.length - 14); tooMany.writeUInt16LE(1025, tooMany.length - 12);
  await assert.rejects(reader.unzip(tooMany), error => error.code === "limit");
  const bomb = zip([["x", "a".repeat(10000)]]);
  const directory = bomb.readUInt32LE(bomb.length - 6);
  bomb.writeUInt32LE(128 * 1024 * 1024 + 1, directory + 24);
  await assert.rejects(reader.unzip(bomb), error => error.code === "limit");
  bomb.writeUInt32LE(2, directory + 24);
  await assert.rejects(reader.unzip(bomb), error => error.code === "limit");
});

test("rejects CRC corruption even for a legacy ZIP", async () => {
  const bytes = zip([["x", "abc"]], { store: true });
  bytes[31] = 120;
  await assert.rejects(core().unzip(bytes), error => error.code === "archive");
});

async function encryptedFixture(entries = bundleEntries()) {
  const bytes = zip(entries), key = new Uint8Array(32).fill(7), nonce = new Uint8Array(12).fill(2);
  const cryptoKey = await webcrypto.subtle.importKey("raw", key, "AES-GCM", false, ["encrypt"]);
  const ciphertext = new Uint8Array(await webcrypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, cryptoKey, bytes));
  return { bytes, ciphertext, key: Buffer.from(key).toString("base64url"), nonce: Buffer.from(nonce).toString("base64url"), sha: hash(ciphertext) };
}
function relay(fixture, overrides = {}) {
  return { v: 1, r: [{ p: "tmpfiles.org", u: "https://tmpfiles.org/dl/123/a.enc" }], k: fixture.key, n: fixture.nonce, s: fixture.sha, b: fixture.ciphertext.length, e: "2099-01-01T00:00:00Z", ...overrides };
}
const relayURL = manifest => "https://share.example/r#h=" + Buffer.from(JSON.stringify(manifest)).toString("base64url");

test("hosted links fetch only their canonical endpoints and keep the fragment key local", async () => {
  const fixture = await encryptedFixture(), requests = [];
  const manifest = { schema: "agent-handoff.link.v1", crypto: { alg: "AES-256-GCM", nonce: fixture.nonce, key_ref: "url-fragment:k" }, bundle: { sha256: fixture.sha, bytes: fixture.ciphertext.length, url: "https://attacker.example/trap" } };
  const result = await core().loadLink({ kind: "hosted", id: "safe" }, "https://share.example/s/safe#k=" + fixture.key + "&extra=ignored", async (url, options) => {
    requests.push({ url, options });
    return new Response(url.endsWith("/blob") ? fixture.ciphertext : JSON.stringify(manifest));
  });
  assert.deepEqual(requests.map(x => x.url), ["https://share.example/v1/shares/safe", "https://share.example/v1/shares/safe/blob"]);
  assert.equal(requests.every(x => x.options.credentials === "omit" && x.options.referrerPolicy === "no-referrer" && x.options.redirect === "error"), true);
  assert.deepEqual(result.bytes, new Uint8Array(fixture.bytes));
  assert.equal(result.command, `agent-handoff import "https://share.example/s/safe#k=${fixture.key}" --execute`);
});

test("relay links validate all capability fields before fetching and retry the next encrypted replica", async () => {
  const fixture = await encryptedFixture(), manifest = relay(fixture, { r: [{ p: "tmpfiles.org", u: "https://tmpfiles.org/dl/1/a.enc" }, { p: "uguu.se", u: "https://a.uguu.se/a.enc" }] });
  const requested = [];
  const result = await core().loadLink({ kind: "relay" }, relayURL(manifest), async (url) => {
    requested.push(url);
    if (url.includes("tmpfiles")) throw new TypeError("CORS");
    if (url === "https://share.example/v1/relay/blob") return new Response("synthetic bridge outage", { status: 502 });
    return new Response(fixture.ciphertext);
  });
  assert.deepEqual(requested, ["https://tmpfiles.org/dl/1/a.enc", "https://share.example/v1/relay/blob", "https://a.uguu.se/a.enc"]);
  assert.deepEqual(result.bytes, new Uint8Array(fixture.bytes));
  for (const overrides of [{ k: "wrong" }, { n: "wrong" }, { e: "2000-01-01T00:00:00Z" }, { b: 65 * 1024 * 1024 }, { r: [{ p: "tmpfiles.org", u: "https://attacker.example/dl/a" }] }, { r: [{ p: "custom-test", u: "https://127.0.0.1/a" }] }]) {
    let contacted = false;
    await assert.rejects(core().loadLink({ kind: "relay" }, relayURL(relay(fixture, overrides)), async () => { contacted = true; return new Response(fixture.ciphertext); }));
    assert.equal(contacted, false);
  }
});

test("temp.sh replicas download with POST like the CLI", async () => {
  const fixture = await encryptedFixture();
  const href = relayURL(relay(fixture, { r: [{ p: "temp.sh", u: "https://temp.sh/synthetic/task.enc" }] }));
  const result = await core().loadLink({ kind: "relay" }, href, async (url, options) => {
    if (url !== "https://temp.sh/synthetic/task.enc" || options.method !== "POST") return new Response("download requires POST", { status: 405 });
    return new Response(fixture.ciphertext);
  });
  assert.deepEqual(result.bytes, new Uint8Array(fixture.bytes));
});

test("a browser CORS failure uses the same-origin ciphertext bridge without sending the fragment capability", async () => {
  const fixture = await encryptedFixture(), href = relayURL(relay(fixture));
  const result = await core().loadLink({ kind: "relay" }, href, async (url, options) => {
    if (url === "https://tmpfiles.org/dl/123/a.enc") throw new TypeError("Failed to fetch: CORS");
    assert.equal(url, "https://share.example/v1/relay/blob");
    assert.equal(options.method, "POST");
    assert.deepEqual(JSON.parse(options.body), { p: "tmpfiles.org", u: "https://tmpfiles.org/dl/123/a.enc", b: fixture.ciphertext.length });
    for (const secret of [fixture.key, fixture.nonce, "#h=", "#k="]) assert.equal((url + options.body).includes(secret), false);
    return new Response(fixture.ciphertext);
  });
  assert.deepEqual(result.bytes, new Uint8Array(fixture.bytes));
  assert.equal(result.command, `agent-handoff import "${href}" --execute`);
  assert.equal((await core().readBundle(result.bytes)).checksummed, true);
});

test("the ciphertext bridge cannot bypass local size, checksum or AES-GCM verification", async () => {
  const fixture = await encryptedFixture();
  for (const mutation of [
    { overrides: { s: "0".repeat(64) }, ciphertext: fixture.ciphertext },
    { overrides: { b: fixture.ciphertext.length - 1 }, ciphertext: fixture.ciphertext },
    { overrides: { k: Buffer.alloc(32, 9).toString("base64url") }, ciphertext: fixture.ciphertext },
    { overrides: {}, ciphertext: fixture.ciphertext.slice(0, -1) },
  ]) {
    await assert.rejects(core().loadLink({ kind: "relay" }, relayURL(relay(fixture, mutation.overrides)), async url => {
      if (url === "https://share.example/v1/relay/blob") return new Response(mutation.ciphertext);
      throw new TypeError("CORS");
    }));
  }
});

test("configured providers remain browser-direct and are never sent through the public bridge", async () => {
  const fixture = await encryptedFixture(), requested = [];
  await assert.rejects(core().loadLink({ kind: "relay" }, relayURL(relay(fixture, { r: [{ p: "config:company", u: "https://files.example/a.enc" }] })), async url => {
    requested.push(url); throw new TypeError("CORS");
  }));
  assert.deepEqual(requested, ["https://files.example/a.enc"]);
});

test("never decrypts a ciphertext with a mismatched size or checksum", async () => {
  const fixture = await encryptedFixture();
  for (const overrides of [{ s: "0".repeat(64) }, { b: fixture.ciphertext.length - 1 }]) {
    await assert.rejects(core().loadLink({ kind: "relay" }, relayURL(relay(fixture, overrides)), async () => new Response(fixture.ciphertext)), error => error.code === "replicas");
  }
});

test("streaming downloads stop when their declared or actual size exceeds the limit", async () => {
  const fixture = await encryptedFixture();
  const response = new Response(new ReadableStream({ start(controller) { controller.enqueue(new Uint8Array(70)); controller.close(); } }), { headers: { "content-length": "67108865" } });
  await assert.rejects(core().loadLink({ kind: "relay" }, relayURL(relay(fixture)), async () => response), error => error.code === "replicas");
});

test("accepts configured providers using the CLI config prefix", async () => {
  const fixture = await encryptedFixture();
  const result = await core().loadLink({ kind: "relay" }, relayURL(relay(fixture, { r: [{ p: "config:company", u: "https://files.example/a.enc" }] })), async () => new Response(fixture.ciphertext));
  assert.deepEqual(result.bytes, new Uint8Array(fixture.bytes));
});

// Node has no DOM. This small boundary adapter implements only native DOM
// operations used by the page; content can never pass through innerHTML.
class Element {
  constructor(tag = "div") { this.tagName = tag; this.children = []; this.listeners = new Map(); this.attributes = new Map(); this.hidden = false; this.disabled = false; this.value = ""; this._text = ""; }
  get textContent() { return this._text + this.children.map(child => child.textContent).join(""); }
  set textContent(value) { this._text = String(value); this.children = []; }
  set innerHTML(_) { throw new Error("untrusted markup reached innerHTML"); }
  appendChild(child) { this.children.push(child); return child; }
  replaceChildren(...children) { this._text = ""; this.children = children; }
  setAttribute(name, value) { this.attributes.set(name, String(value)); }
  getAttribute(name) { return this.attributes.get(name); }
  addEventListener(type, callback) { const callbacks = this.listeners.get(type) || []; callbacks.push(callback); this.listeners.set(type, callbacks); }
  async fire(type) { for (const callback of this.listeners.get(type) || []) await callback({ currentTarget: this }); }
  focus() { this.focused = true; }
  select() { this.selected = true; }
}
function pageScope(href, ciphertext) {
  const elements = Object.fromEntries(["language", "privacy", "subtitle", "status", "error", "retry", "download", "command", "copy", "share-link", "copy-command", "command-label", "install-label", "install-text", "docs", "import-label", "fallback", "content", "verified", "heading"].map(id => [id, new Element(id === "command" ? "textarea" : "div")]));
  elements.heading.textContent = "Shared session";
  elements.download.disabled = true;
  const copied = [], requests = [];
  class LocalURL extends URL { static createObjectURL() { return "blob:local-download"; } static revokeObjectURL() {} }
  const scope = {
    document: { title: "", documentElement: {}, getElementById: id => elements[id], createElement: tag => new Element(tag), querySelector: () => elements.heading },
    location: { href }, navigator: { language: "en", clipboard: { writeText: async text => { copied.push(text); } } },
    fetch: async url => { requests.push(url); return new Response(ciphertext); }, URL: LocalURL, Blob, addEventListener() {},
  };
  return { elements, scope, copied, requests };
}
const descendants = node => [node, ...node.children.flatMap(descendants)];

test("the readable page uses inert text, lazy tool evidence, a downloadable ZIP and the full copyable import command", async () => {
  assert.equal(typeof module.mountReader, "function", "the shared reader page must be implemented");
  const malicious = '<img src="https://attacker.example/track" onerror="alert(1)">';
  const fixture = await encryptedFixture(bundleEntries({
    "manifest.json": JSON.stringify({ format_version: 2, artifact_type: "agent-handoff", title: malicious }),
    "agent/neutral.json": JSON.stringify({ schema: "agent-handoff.neutral.v1", entries: [{ kind: "message", role: "user", text: malicious + " [remote image](https://attacker.example/image)" }, { kind: "tool", tool: "exec", status: "completed", input: "go test ./...", output: "PASS" }, { kind: "message", role: "assistant", text: "Reviewed the literal text." }] }),
    "code/manifest.json": JSON.stringify({ schema: "agent-handoff.code.v1", files: [{ path: malicious, status: "M", bytes: 5 }] }),
  }));
  const href = relayURL(relay(fixture)), { elements, scope, copied, requests } = pageScope(href, fixture.ciphertext);
  await module.mountReader(core(), { kind: "relay" }, scope);
  assert.equal(elements.heading.textContent, malicious);
  assert.match(elements.content.textContent, /Reviewed the literal text/);
  assert.match(elements.content.textContent, /<img src=/);
  assert.equal(descendants(elements.content).some(node => ["img", "a", "script", "iframe"].includes(node.tagName)), false);
  assert.deepEqual(requests, ["https://tmpfiles.org/dl/123/a.enc"]);
  assert.equal(elements.download.disabled, false);
  assert.equal(elements.download.href, "blob:local-download");
  const tool = descendants(elements.content).find(node => node.tagName === "details" && node.textContent.includes("exec"));
  assert.ok(tool);
  assert.equal(tool.textContent.includes("PASS"), false);
  tool.open = true; await tool.fire("toggle");
  assert.match(tool.textContent, /go test \.\/\.\.\./);
  assert.match(tool.textContent, /PASS/);
  await elements.copy.fire("click");
  assert.equal(copied[0], href);
  await elements["copy-command"].fire("click");
  assert.equal(copied[1], `agent-handoff import "${href}" --execute`);
  elements.language.value = "zh"; await elements.language.fire("change");
  assert.match(elements.content.textContent, /对话/);
});

test("invalid links show the recovery interface without offering an empty copy command", async () => {
  const { elements, scope } = pageScope("https://share.example/r", new Uint8Array());
  await module.mountReader(core(), { kind: "relay" }, scope);
  assert.equal(elements.error.hidden, false);
  assert.equal(elements.copy.disabled, true);
  assert.equal(elements.retry.disabled, false);
});

test("load errors appear before any navigation and all surrounding reader text follows the selected language", async () => {
  const fixture = await encryptedFixture(), href = relayURL(relay(fixture));
  const { elements, scope, copied } = pageScope(href, fixture.ciphertext);
  scope.fetch = async () => { throw new TypeError("CORS"); };
  scope.navigator.language = "zh-CN";
  await module.mountReader(core(), { kind: "relay" }, scope);
  assert.equal(elements.error.hidden, false);
  assert.equal(elements.heading.textContent, "共享任务");
  assert.equal(elements.docs.textContent, "源码与文档");
  assert.match(elements["install-label"].textContent, /首次/);
  assert.match(elements["install-text"].textContent, /安装/);
  assert.equal(/CORS|跨域/.test(elements.error.textContent), false);
  await elements.copy.fire("click");
  assert.equal(copied[0], href);
  elements.language.value = "en"; await elements.language.fire("change");
  assert.equal(elements.heading.textContent, "Shared session");
  assert.equal(elements.docs.textContent, "Source code and documentation");
});

test("the command is collapsed and documentation opens separately so the reader remains available", async () => {
  const response = readerModule.readerPageResponse({ kind: "relay" }, "Shared session", value => value);
  const html = await response.text();
  const details = html.match(/<details[^>]*>\s*<summary id="command-label">[\s\S]*?<\/details>/)?.[0];
  assert.ok(details);
  assert.equal(/<details[^>]*\bopen\b/.test(details), false);
  assert.match(details, /id="command"/);
  const source = html.match(/<a[^>]*id="docs"[^>]*>/)?.[0];
  assert.ok(source);
  assert.match(source, /href="https:\/\/github\.com\/DavidDingXu\/agent-handoff"/);
  assert.match(source, /target="_blank"/);
  assert.match(source, /rel="noopener noreferrer"/);
});

test("a failed checksum renders no archive text, keeps ZIP download available and permits retry", async () => {
  assert.equal(typeof module.mountReader, "function", "the shared reader page must be implemented");
  const entries = bundleEntries().map(([name, bytes]) => [name, name === "agent/neutral.json" ? String(bytes).replace("Login is fixed.", "TAMPERED CONTENT") : bytes]);
  const fixture = await encryptedFixture(entries), { elements, scope } = pageScope(relayURL(relay(fixture)), fixture.ciphertext);
  await module.mountReader(core(), { kind: "relay" }, scope);
  assert.equal(elements.content.textContent, "");
  assert.equal(elements.error.hidden, false);
  assert.equal(elements.error.textContent.includes("TAMPERED"), false);
  assert.equal(elements.download.disabled, false);
  assert.equal(elements.retry.disabled, false);
  assert.match(elements.command.value, /^agent-handoff import "https:/);
});

test("long transcripts load in batches and do not create an unbounded DOM", async () => {
  assert.equal(typeof module.mountReader, "function", "the shared reader page must be implemented");
  const entries = Array.from({ length: 240 }, (_, index) => ({ kind: "message", role: "user", text: "Message " + index }));
  const fixture = await encryptedFixture(bundleEntries({ "agent/neutral.json": JSON.stringify({ schema: "agent-handoff.neutral.v1", entries }) }));
  const { elements, scope } = pageScope(relayURL(relay(fixture)), fixture.ciphertext);
  await module.mountReader(core(), { kind: "relay" }, scope);
  const conversation = elements.content.children.find(node => node.children[0]?.textContent === "Conversation");
  assert.ok(conversation);
  assert.equal(conversation.textContent.includes("Message 239"), false);
  const more = descendants(conversation).find(node => node.tagName === "button");
  assert.ok(more);
  await more.fire("click");
  const nextPage = elements.content.children.find(node => node.children[0]?.textContent === "Conversation");
  assert.equal(nextPage.textContent.includes("Message 239"), true);
});

test("Wrangler's minified deployed page still decrypts and renders the same handoff", async () => {
  const temporary = await mkdtemp(join(tmpdir(), "agent-handoff-reader-test-"));
  try {
    const config = join(temporary, "wrangler.json");
    await writeFile(config, JSON.stringify({ name: "agent-handoff-reader-test", main: resolve("src/index.js"), compatibility_date: "2026-08-24" }));
    await promisify(execFile)(process.execPath, ["node_modules/wrangler/bin/wrangler.js", "deploy", "--config", config, "--dry-run", "--minify", "--outdir", temporary], { timeout: 30000, env: { ...process.env, WRANGLER_SEND_METRICS: "false" } });
    const source = await readFile(join(temporary, "index.js"), "utf8");
    const bundled = await import("data:text/javascript;base64," + Buffer.from(source).toString("base64"));
    const response = await bundled.default.fetch(new Request("https://share.example/r"), {}, {});
    const html = await response.text(), script = html.match(/<script nonce="[^"]+">([\s\S]*?)<\/script>/)?.[1];
    assert.ok(script, "the bundled response must contain a local reader script");
    const fixture = await encryptedFixture(), { elements, scope } = pageScope(relayURL(relay(fixture)), fixture.ciphertext);
    await vm.runInNewContext(script, { ...scope, ...browserGlobals });
    assert.match(elements.content.textContent, /Repair the login flow/);
    assert.equal(elements.error.hidden, true);
    // Exercise the bundled server route inside workerd, with synthetic providers
    // behind its real fetch boundary (no network or real share is contacted).
    const { Miniflare, convertV4MiniflareOptions } = await import("miniflare");
    const upstream = [];
    const runtime = new Miniflare(convertV4MiniflareOptions({
      modules: true, script: source, compatibilityDate: "2026-08-24",
      outboundService: async request => {
        upstream.push({ url: request.url, method: request.method });
        assert.equal(request.url.includes(fixture.key), false);
        if (new URL(request.url).hostname === "filebin.net") return new Response(null, { status: 302, headers: { location: "https://s3.filebin.net/synthetic/task.enc?signature=synthetic" } });
        return new Response(fixture.ciphertext);
      },
    }));
    try {
      for (const [p, u] of [["tmpfiles.org", "https://tmpfiles.org/dl/123/a.enc"], ["temp.sh", "https://temp.sh/synthetic/task.enc"], ["filebin.net", "https://filebin.net/synthetic/task.enc"], ["uguu.se", "https://a.uguu.se/synthetic.enc"]]) {
        const shared = relayURL(relay(fixture, { r: [{ p, u }] }));
        const loaded = await core().loadLink({ kind: "relay" }, shared, async (url, options) => {
          if (new URL(url).origin !== "https://share.example") throw new TypeError("synthetic browser CORS failure");
          return runtime.dispatchFetch(url, options);
        });
        assert.deepEqual(loaded.bytes, new Uint8Array(fixture.bytes));
        assert.equal((await core().readBundle(loaded.bytes)).checksummed, true);
      }
      assert.equal(upstream.find(request => new URL(request.url).hostname === "temp.sh").method, "POST");
      assert.equal(upstream.some(request => request.url.startsWith("https://s3.filebin.net/")), true);
    } finally { await runtime.dispose(); }
  } finally { await rm(temporary, { recursive: true, force: true }); }
});
