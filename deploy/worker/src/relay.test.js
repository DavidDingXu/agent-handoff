import assert from "node:assert/strict";
import test from "node:test";
import worker from "./index.js";

const locator = { p: "tmpfiles.org", u: "https://tmpfiles.org/dl/123/synthetic.enc", b: 4 };
const request = value => new Request("https://share.example/v1/relay/blob", { method: "POST", headers: { "content-type": "application/json", cookie: "private=user", authorization: "Bearer private" }, body: JSON.stringify(value) });

test("the relay bridge returns only bounded ciphertext for the four built-in download protocols", async t => {
  for (const [p, u, method] of [
    ["tmpfiles.org", locator.u, "GET"],
    ["filebin.net", "https://filebin.net/synthetic/task.enc", "GET"],
    ["uguu.se", "https://a.uguu.se/synthetic.enc", "GET"],
    ["temp.sh", "https://temp.sh/synthetic/task.enc", "POST"],
  ]) {
    t.mock.method(globalThis, "fetch", async (url, options) => {
      assert.equal(String(url), u);
      assert.equal(options.method, method);
      assert.equal(options.redirect, "manual");
      const headers = new Headers(options.headers);
      assert.equal(headers.get("authorization"), null);
      assert.equal(headers.get("cookie"), null);
      assert.equal(headers.get("referer"), null);
      assert.equal(options.body, undefined);
      return new Response(new Uint8Array([1, 2, 3, 4]), { headers: { "set-cookie": "provider=private", "content-type": "text/html" } });
    });
    const response = await worker.fetch(request({ p, u, b: 4 }), {}, {});
    assert.equal(response.status, 200);
    assert.deepEqual(new Uint8Array(await response.arrayBuffer()), new Uint8Array([1, 2, 3, 4]));
    assert.equal(response.headers.get("content-type"), "application/octet-stream");
    assert.equal(response.headers.get("cache-control"), "no-store");
    assert.equal(response.headers.get("set-cookie"), null);
    t.mock.restoreAll();
  }
});

test("the relay bridge rejects arbitrary URLs, capability fields and upload paths before fetching", async t => {
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => { calls++; throw new Error("must not fetch"); });
  for (const value of [
    { ...locator, p: "config:company", u: "https://files.example/task.enc" },
    { ...locator, u: "https://attacker.example/dl/123/task.enc" },
    { ...locator, u: "http://tmpfiles.org/dl/123/task.enc" },
    { ...locator, u: "https://127.0.0.1/dl/123/task.enc" },
    { ...locator, u: "https://tmpfiles.org:8443/dl/123/task.enc" },
    { ...locator, u: "https://user@tmpfiles.org/dl/123/task.enc" },
    { ...locator, u: "https://tmpfiles.org/dl/123/task.enc#k=secret" },
    { ...locator, u: "https://tmpfiles.org/dl/123/task.enc?k=secret" },
    { ...locator, u: "https://tmpfiles.org/api/v1/upload" },
    { ...locator, p: "temp.sh", u: "https://temp.sh/upload" },
    { ...locator, p: "temp.sh", u: "https://temp.sh/upload/../upload" },
    { ...locator, p: "temp.sh", u: "https://temp.sh/a%2fb/task.enc" },
    { ...locator, k: "fragment-key-must-not-be-accepted" },
    { ...locator, h: "whole-capability-must-not-be-accepted" },
    { ...locator, b: 0 }, { ...locator, b: 1.5 }, { ...locator, b: 64 * 1024 * 1024 + 1 },
    null,
  ]) {
    const response = await worker.fetch(request(value), {}, {});
    assert.equal(response.status, 400, JSON.stringify(value));
  }
  const oversized = request({ ...locator, padding: "x".repeat(8192) });
  assert.equal((await worker.fetch(oversized, {}, {})).status, 400);
  assert.equal(calls, 0);
});

test("the public bridge refuses calls from other browser origins", async t => {
  let contacted = false;
  t.mock.method(globalThis, "fetch", async () => { contacted = true; return new Response(new Uint8Array(4)); });
  for (const headers of [{ origin: "https://attacker.example" }, { origin: "null" }, { "sec-fetch-site": "cross-site" }]) {
    const input = request(locator);
    for (const [name, value] of Object.entries(headers)) input.headers.set(name, value);
    const response = await worker.fetch(input, {}, {});
    assert.equal(response.status, 400);
  }
  assert.equal(contacted, false);
});

test("Filebin redirects stay on its provider domains and never forward client credentials", async t => {
  const calls = [];
  t.mock.method(globalThis, "fetch", async (url, options) => {
    calls.push(String(url));
    assert.equal(options.redirect, "manual");
    assert.equal(new Headers(options.headers).has("cookie"), false);
    if (calls.length === 1) return new Response(null, { status: 302, headers: { location: "https://s3.filebin.net/synthetic/task.enc?X-Amz-Signature=synthetic" } });
    return new Response(new Uint8Array([1, 2, 3, 4]));
  });
  const response = await worker.fetch(request({ p: "filebin.net", u: "https://filebin.net/synthetic/task.enc", b: 4 }), {}, {});
  assert.equal(response.status, 200);
  assert.equal((await response.arrayBuffer()).byteLength, 4);
  assert.equal(calls.length, 2);
});

test("Filebin's public verification cookie is retried once and stays on Filebin", async t => {
  let calls = 0;
  t.mock.method(globalThis, "fetch", async (url, options) => {
    calls++;
    const cookie = new Headers(options.headers).get("cookie");
    if (calls === 1) return new Response("<html>Download warning</html>", { headers: { "content-type": "text/html", "set-cookie": "verified=2024-05-24; Path=/; Secure; HttpOnly" } });
    if (calls === 2) {
      assert.equal(String(url), "https://filebin.net/synthetic/task.enc");
      assert.equal(cookie, "verified=2024-05-24");
      return new Response(null, { status: 302, headers: { location: "https://s3.filebin.net/synthetic/task.enc?signature=synthetic" } });
    }
    assert.equal(cookie, null);
    return new Response(new Uint8Array([1, 2, 3, 4]));
  });
  const response = await worker.fetch(request({ p: "filebin.net", u: "https://filebin.net/synthetic/task.enc", b: 4 }), {}, {});
  assert.deepEqual(new Uint8Array(await response.arrayBuffer()), new Uint8Array([1, 2, 3, 4]));
  assert.equal(calls, 3);
  assert.equal(response.headers.get("set-cookie"), null);
});

test("temporary-store redirects preserve POST only for 307 and 308", async t => {
  for (const status of [301, 302, 303, 307, 308]) {
    let calls = 0;
    t.mock.method(globalThis, "fetch", async (_url, options) => {
      calls++;
      if (calls === 1) return new Response(null, { status, headers: { location: "/synthetic/redirected.enc" } });
      assert.equal(options.method, status === 307 || status === 308 ? "POST" : "GET");
      return new Response(new Uint8Array([1, 2, 3, 4]));
    });
    const response = await worker.fetch(request({ p: "temp.sh", u: "https://temp.sh/synthetic/task.enc", b: 4 }), {}, {});
    assert.equal(response.status, 200, String(status));
    assert.equal((await response.arrayBuffer()).byteLength, 4);
    t.mock.restoreAll();
  }
});

test("the relay bridge refuses unsafe redirects and stops redirect loops", async t => {
  for (const target of ["http://tmpfiles.org/dl/123/task.enc", "https://127.0.0.1/", "https://localhost/", "https://attacker.example/", "https://temp.sh/synthetic/task.enc", "https://tmpfiles.org:8443/", "https://user@tmpfiles.org/dl/123/task.enc", "https://tmpfiles.org/dl/123/task.enc#k=secret"]) {
    let calls = 0;
    t.mock.method(globalThis, "fetch", async () => { calls++; return new Response(null, { status: 302, headers: { location: target } }); });
    const response = await worker.fetch(request(locator), {}, {});
    assert.equal(response.status, 502, target);
    assert.equal(calls, 1, target);
    t.mock.restoreAll();
  }
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => { calls++; return new Response(null, { status: 302, headers: { location: locator.u } }); });
  assert.equal((await worker.fetch(request(locator), {}, {})).status, 502);
  assert.ok(calls <= 4);
});

test("the relay bridge stops oversized and truncated upstream bodies", async t => {
  for (const headers of [{ "content-length": "5" }, { "content-length": "invalid" }]) {
    t.mock.method(globalThis, "fetch", async () => new Response(new Uint8Array(5), { headers }));
    assert.equal((await worker.fetch(request(locator), {}, {})).status, 502);
    t.mock.restoreAll();
  }
  for (const size of [3, 5]) {
    t.mock.method(globalThis, "fetch", async () => new Response(new Uint8Array(size)));
    const response = await worker.fetch(request(locator), {}, {});
    await assert.rejects(response.arrayBuffer());
    t.mock.restoreAll();
  }
});

test("a stalled relay response is aborted within the download deadline", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let started;
  const ready = new Promise(resolve => { started = resolve; });
  t.mock.method(globalThis, "fetch", async (_url, options) => {
    started();
    return new Promise((_resolve, reject) => options.signal.addEventListener("abort", () => reject(new Error("aborted")), { once: true }));
  });
  const pending = worker.fetch(request(locator), {}, {});
  await Promise.race([ready, pending.then(response => assert.equal(response.status, 502))]);
  t.mock.timers.tick(20001);
  assert.equal((await pending).status, 502);
});

test("the deadline also cancels a stalled ciphertext stream after headers arrive", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let cancelled = false;
  t.mock.method(globalThis, "fetch", async () => new Response(new ReadableStream({ cancel() { cancelled = true; } })));
  const response = await worker.fetch(request(locator), {}, {});
  assert.equal(response.status, 200);
  const body = response.arrayBuffer();
  t.mock.timers.tick(20001);
  await assert.rejects(body);
  assert.equal(cancelled, true);
});
