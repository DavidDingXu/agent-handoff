// Browser fallback for the built-in anonymous stores. This accepts a ciphertext
// locator only; capabilities, decryption keys and plaintext stay in the browser.
function downloadURL(provider, raw, redirect = false) {
  if (typeof raw !== "string" || raw.length > 4096) throw new Error("invalid locator");
  const url = new URL(raw);
  if (url.protocol !== "https:" || url.username || url.password || url.port || url.hash || (!redirect && url.search)) throw new Error("invalid locator");
  const host = url.hostname, path = url.pathname;
  const segments = path.split("/").slice(1);
  if (!segments.length || segments.some(part => !/^[A-Za-z0-9._~-]+$/.test(part) || part === "." || part === "..")) throw new Error("invalid path");
  if (provider === "filebin.net") {
    if (redirect && /^[a-z0-9-]+\.filebin\.net$/.test(host)) return url;
    if (host === "filebin.net" && segments.length === 2) return url;
  } else if (provider === "tmpfiles.org") {
    if (host === "tmpfiles.org" && segments.length === 3 && segments[0] === "dl") return url;
  } else if (provider === "uguu.se") {
    if (/^[a-z0-9-]+\.uguu\.se$/.test(host) && segments.length === 1) return url;
  } else if (provider === "temp.sh") {
    if (host === "temp.sh" && segments.length === 2) return url;
  }
  throw new Error("unsupported locator");
}

export async function relayCiphertextResponse(request) {
  const abort = new AbortController();
  let reader, output, finished = false, validated = false;
  function finish() {
    if (finished) return;
    finished = true; clearTimeout(timeout); abort.abort();
    if (reader) void reader.cancel().catch(() => {});
    request.signal.removeEventListener("abort", cancel);
  }
  function cancel() {
    if (finished) return;
    if (output) output.error(new Error("relay download interrupted"));
    finish();
  }
  const timeout = setTimeout(cancel, 20000);
  request.signal.addEventListener("abort", cancel, { once: true });
  try {
    const origin = request.headers.get("origin");
    if ((origin && origin !== new URL(request.url).origin) || request.headers.get("sec-fetch-site") === "cross-site") throw new Error("invalid origin");
    if (request.signal.aborted || !/^application\/json(?:\s*;|$)/i.test(request.headers.get("content-type") || "") || !request.body) throw new Error("invalid request");
    // Do not trust Content-Length: cap the actual request body before JSON.parse.
    reader = request.body.getReader();
    const input = new Uint8Array(8192); let used = 0;
    while (true) {
      const chunk = await reader.read();
      if (finished) throw new Error("interrupted");
      if (chunk.done) break;
      if (chunk.value.length > input.length - used) throw new Error("request too large");
      input.set(chunk.value, used); used += chunk.value.length;
    }
    const locator = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(input.subarray(0, used)));
    if (!locator || typeof locator !== "object" || Array.isArray(locator) || Object.keys(locator).sort().join(",") !== "b,p,u" || !Number.isSafeInteger(locator.b) || locator.b <= 0 || locator.b > 64 * 1024 * 1024) throw new Error("invalid locator");
    let url = downloadURL(locator.p, locator.u), method = locator.p === "temp.sh" ? "POST" : "GET", verification = "";
    validated = true;
    for (let redirects = 0; redirects <= 3; redirects++) {
      const response = await fetch(url.href, {
        method, redirect: "manual", signal: abort.signal, cache: "no-store",
        headers: { accept: "application/octet-stream", "user-agent": "agent-handoff/relay", ...(url.hostname === "filebin.net" && verification ? { cookie: verification } : {}) },
      });
      if ([301, 302, 303, 307, 308].includes(response.status)) {
        if (response.body) void response.body.cancel().catch(() => {});
        const location = response.headers.get("location");
        if (!location || redirects === 3) throw new Error("invalid redirect");
        url = downloadURL(locator.p, new URL(location, url).href, true);
        if ([301, 302, 303].includes(response.status)) method = "GET";
        continue;
      }
      // Filebin documents a public download-warning cookie before its 302.
      // Retry only that cookie, once, on the same host; never forward user cookies.
      const cookie = response.headers.get("set-cookie")?.match(/^verified=[A-Za-z0-9._-]{1,128}(?=;|$)/)?.[0];
      if (url.hostname === "filebin.net" && !verification && response.status === 200 && response.headers.get("content-type")?.startsWith("text/html") && cookie) {
        if (response.body) void response.body.cancel().catch(() => {});
        verification = cookie; continue;
      }
      reader = response.body?.getReader();
      if (response.status !== 200 || !reader) throw new Error("download failed");
      const length = response.headers.get("content-length");
      if (length !== null && (!/^\d+$/.test(length) || Number(length) !== locator.b)) throw new Error("invalid size");
      let received = 0;
      const body = new ReadableStream({
        start(controller) { output = controller; },
        async pull(controller) {
          try {
            const chunk = await reader.read();
            if (finished) return;
            if (chunk.done) {
              if (received !== locator.b) throw new Error("invalid size");
              controller.close(); finish(); return;
            }
            if (chunk.value.length > locator.b - received) throw new Error("invalid size");
            received += chunk.value.length; controller.enqueue(chunk.value);
          } catch {
            if (!finished) { controller.error(new Error("relay download failed")); finish(); }
          }
        },
        cancel() { finish(); },
      });
      return new Response(body, { headers: {
        "content-type": "application/octet-stream", "cache-control": "no-store", "x-content-type-options": "nosniff", "referrer-policy": "no-referrer",
      } });
    }
    throw new Error("download failed");
  } catch {
    finish();
    return new Response(JSON.stringify({ error: validated ? "relay_download_failed" : "invalid_relay_locator" }), { status: validated ? 502 : 400, headers: { "content-type": "application/json", "cache-control": "no-store" } });
  }
}
