// Single browser source of truth. Keeping this as raw text prevents Wrangler's
// name-preservation/minification helpers from leaking into the inline script.
// It is embedded locally under a CSP nonce; no runtime eval or Function is used.
export const browserReaderSource = String.raw`function createReaderCore() {
  const limits = { encrypted: 64 * 1024 * 1024, entry: 128 * 1024 * 1024, expanded: 256 * 1024 * 1024, json: 16 * 1024 * 1024, entries: 1024, transcript: 100000 };
  const fail = code => { const error = new Error(code); error.code = code; throw error; };
  const object = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const utf8 = bytes => { try { return new TextDecoder("utf-8", { fatal: true }).decode(bytes); } catch { fail("archive"); } };
  const json = bytes => { if (bytes.length > limits.json) fail("limit"); try { return JSON.parse(utf8(bytes)); } catch { fail("archive"); } };
  const validPath = name => typeof name === "string" && name.length > 0 && !/[\\:\u0000-\u001f\u007f]/.test(name) && name.split("/").every(part => part && part !== "." && part !== "..");
  const hex = bytes => Array.from(bytes, byte => byte.toString(16).padStart(2, "0")).join("");
  const sha256 = async bytes => hex(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)));
  function decode64(value, expected) {
    if (typeof value !== "string" || !/^[A-Za-z0-9_-]+$/.test(value)) fail("link");
    let decoded;
    try { decoded = Uint8Array.from(atob(value.replace(/-/g, "+").replace(/_/g, "/")), char => char.charCodeAt(0)); } catch { fail("link"); }
    if (expected !== undefined && decoded.length !== expected) fail("link");
    return decoded;
  }

  // A bounded raw DEFLATE decoder avoids depending on deflate-raw support in
  // DecompressionStream. Output is allocated only after ZIP limits are checked;
  // every literal/back-reference is checked against the declared entry size.
  function inflateRaw(data, size) {
    let input = 0, buffer = 0, bits = 0, output = 0;
    const out = new Uint8Array(size);
    function read(count) {
      while (bits < count) {
        if (input >= data.length) fail("archive");
        buffer |= data[input++] << bits; bits += 8;
      }
      const value = buffer & ((1 << count) - 1);
      buffer >>>= count; bits -= count;
      return value;
    }
    function tree(lengths) {
      const counts = new Uint16Array(16), next = new Uint16Array(16);
      let max = 0, left = 1, code = 0;
      for (const length of lengths) { if (length > 15) fail("archive"); counts[length]++; max = Math.max(max, length); }
      if (!max) return { table: new Uint32Array(1), max: 0 };
      for (let length = 1; length <= 15; length++) {
        left = (left << 1) - counts[length]; if (left < 0) fail("archive");
        code = (code + (length === 1 ? 0 : counts[length - 1])) << 1; next[length] = code;
      }
      const table = new Uint32Array(1 << max);
      lengths.forEach((length, symbol) => {
        if (!length) return;
        let canonical = next[length]++, reversed = 0;
        for (let bit = 0; bit < length; bit++) { reversed = (reversed << 1) | (canonical & 1); canonical >>>= 1; }
        for (let index = reversed; index < table.length; index += 1 << length) table[index] = (length << 16) | symbol;
      });
      return { table, max };
    }
    function symbol(huffman) {
      while (bits < huffman.max && input < data.length) { buffer |= data[input++] << bits; bits += 8; }
      const value = huffman.table[buffer & ((1 << huffman.max) - 1)], count = value >>> 16;
      if (!count || count > bits) fail("archive");
      buffer >>>= count; bits -= count;
      return value & 0xffff;
    }
    const lengthBase = [3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258];
    const lengthBits = [0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0];
    const distanceBase = [1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577];
    const distanceBits = [0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13];
    let final;
    do {
      final = read(1);
      const type = read(2);
      if (type === 0) {
        const padding = bits % 8; buffer >>>= padding; bits -= padding;
        const count = read(16), complement = read(16);
        if ((count ^ 0xffff) !== complement) fail("archive");
        if (count > size - output) fail("limit");
        for (let index = 0; index < count; index++) out[output++] = read(8);
        continue;
      }
      let literals, distances;
      if (type === 1) {
        literals = tree(Array.from({ length: 288 }, (_, index) => index < 144 ? 8 : index < 256 ? 9 : index < 280 ? 7 : 8));
        distances = tree(new Array(32).fill(5));
      } else if (type === 2) {
        const literalCount = read(5) + 257, distanceCount = read(5) + 1, codeCount = read(4) + 4;
        if (literalCount > 286) fail("archive");
        const order = [16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15], codeLengths = new Array(19).fill(0);
        for (let index = 0; index < codeCount; index++) codeLengths[order[index]] = read(3);
        const codes = tree(codeLengths), lengths = [];
        while (lengths.length < literalCount + distanceCount) {
          const value = symbol(codes);
          if (value < 16) lengths.push(value);
          else {
            if (value === 16 && !lengths.length) fail("archive");
            const repeated = value === 16 ? lengths[lengths.length - 1] : 0;
            const count = value === 16 ? read(2) + 3 : value === 17 ? read(3) + 3 : read(7) + 11;
            if (lengths.length + count > literalCount + distanceCount) fail("archive");
            for (let index = 0; index < count; index++) lengths.push(repeated);
          }
        }
        if (!lengths[256]) fail("archive");
        literals = tree(lengths.slice(0, literalCount)); distances = tree(lengths.slice(literalCount));
      } else fail("archive");
      while (true) {
        const value = symbol(literals);
        if (value === 256) break;
        if (value < 256) { if (output >= size) fail("limit"); out[output++] = value; continue; }
        if (value > 285) fail("archive");
        const length = lengthBase[value - 257] + read(lengthBits[value - 257]);
        const distanceCode = symbol(distances);
        if (distanceCode >= 30) fail("archive");
        const distance = distanceBase[distanceCode] + read(distanceBits[distanceCode]);
        if (distance > output) fail("archive");
        if (length > size - output) fail("limit");
        for (let index = 0; index < length; index++) { out[output] = out[output - distance]; output++; }
      }
    } while (!final);
    if (output !== size || input !== data.length || bits > 7) fail("archive");
    return out;
  }

  const crcTable = Uint32Array.from({ length: 256 }, (_, index) => {
    let value = index;
    for (let bit = 0; bit < 8; bit++) value = (value >>> 1) ^ (value & 1 ? 0xedb88320 : 0);
    return value;
  });
  function crc32(bytes) {
    let value = 0xffffffff;
    for (const byte of bytes) value = (value >>> 8) ^ crcTable[(value ^ byte) & 255];
    return (value ^ 0xffffffff) >>> 0;
  }
  async function unzip(input) {
    const bytes = input instanceof Uint8Array ? input : new Uint8Array(input);
    if (bytes.length > limits.encrypted) fail("limit");
    if (bytes.length < 22) fail("archive");
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    const u16 = offset => view.getUint16(offset, true), u32 = offset => view.getUint32(offset, true);
    let end = bytes.length - 22;
    for (; end >= Math.max(0, bytes.length - 65557); end--) if (u32(end) === 0x06054b50 && end + 22 + u16(end + 20) === bytes.length) break;
    if (end < 0 || u32(end) !== 0x06054b50 || u16(end + 4) || u16(end + 6) || u16(end + 8) !== u16(end + 10)) fail("archive");
    const count = u16(end + 10), directory = u32(end + 16), directorySize = u32(end + 12);
    if (count > limits.entries) fail("limit");
    if (!count || directory + directorySize !== end) fail("archive");
    let position = directory, expanded = 0;
    const entries = [], seen = new Set();
    for (let index = 0; index < count; index++) {
      if (position + 46 > end || u32(position) !== 0x02014b50) fail("archive");
      const flags = u16(position + 8), method = u16(position + 10), checksum = u32(position + 16);
      const compressed = u32(position + 20), size = u32(position + 24), nameLength = u16(position + 28);
      const next = position + 46 + nameLength + u16(position + 30) + u16(position + 32), local = u32(position + 42);
      if (next > end || u16(position + 34) || (flags & 1) || (method !== 0 && method !== 8)) fail("archive");
      const nameBytes = bytes.subarray(position + 46, position + 46 + nameLength), name = utf8(nameBytes);
      if (!validPath(name) || seen.has(name)) fail("archive");
      seen.add(name);
      if (size > limits.entry || size > limits.expanded - expanded) fail("limit");
      expanded += size;
      if (local + 30 > directory || u32(local) !== 0x04034b50 || u16(local + 6) !== flags || u16(local + 8) !== method || u16(local + 26) !== nameLength) fail("archive");
      const start = local + 30 + nameLength + u16(local + 28);
      if (start + compressed > directory || utf8(bytes.subarray(local + 30, local + 30 + nameLength)) !== name) fail("archive");
      if (!(flags & 8) && (u32(local + 14) !== checksum || u32(local + 18) !== compressed || u32(local + 22) !== size)) fail("archive");
      entries.push({ name, local, start, compressed, size, method, checksum }); position = next;
    }
    if (position !== end) fail("archive");
    const ranges = [...entries].sort((a, b) => a.local - b.local);
    for (let index = 1; index < ranges.length; index++) if (ranges[index].local < ranges[index - 1].start + ranges[index - 1].compressed) fail("archive");
    const files = new Map();
    for (const entry of entries) {
      const compressed = bytes.subarray(entry.start, entry.start + entry.compressed);
      const data = entry.method === 0 ? compressed : inflateRaw(compressed, entry.size);
      if (data.length !== entry.size || crc32(data) !== entry.checksum) fail("archive");
      files.set(entry.name, data);
    }
    return files;
  }

  function legacyTranscript(bytes) {
    if (bytes && bytes.length > limits.json) fail("limit");
    const entries = [], pending = new Map();
    const append = entry => {
      if (entries.length >= limits.transcript) fail("limit");
      const previous = entries[entries.length - 1];
      if (entry.kind === "message" && previous?.kind === "message" && previous.role === entry.role && previous.text === entry.text) return;
      entries.push(entry);
    };
    const source = utf8(bytes || new Uint8Array());
    let position = 0, records = 0;
    while (position < source.length) {
      if (++records > limits.transcript) fail("limit");
      const end = source.indexOf("\n", position), next = end < 0 ? source.length : end;
      const line = source.slice(position, next); position = next + 1;
      let item; try { item = JSON.parse(line); } catch { continue; }
      const payload = item?.payload;
      if (!object(payload)) continue;
      if (item.type === "event_msg" && ["user_message", "agent_message"].includes(payload.type) && typeof payload.message === "string") {
        append({ kind: "message", role: payload.type === "user_message" ? "user" : "assistant", text: payload.message });
      } else if (item.type === "response_item" && payload.type === "message" && ["user", "assistant"].includes(payload.role)) {
        const text = Array.isArray(payload.content) ? payload.content.filter(part => ["input_text", "output_text", "text"].includes(part?.type) && typeof part.text === "string").map(part => part.text).join("\n") : "";
        if (text) append({ kind: "message", role: payload.role, text });
      } else if (item.type === "response_item" && ["function_call", "custom_tool_call"].includes(payload.type)) {
        const entry = { kind: "tool", tool: payload.name, status: "called", input: typeof payload.arguments === "string" ? payload.arguments : typeof payload.input === "string" ? payload.input : "" };
        append(entry); if (typeof payload.call_id === "string") pending.set(payload.call_id, entry);
      } else if (item.type === "response_item" && ["function_call_output", "custom_tool_call_output"].includes(payload.type)) {
        const entry = pending.get(payload.call_id);
        if (entry) { entry.output = typeof payload.output === "string" ? payload.output : ""; entry.status = payload.is_error ? "failed" : "completed"; pending.delete(payload.call_id); }
      }
    }
    return { schema: "agent-handoff.neutral.v1", entries };
  }

  async function readBundle(bytes) {
    const files = await unzip(bytes);
    if (!files.has("manifest.json")) fail("archive");
    const manifest = json(files.get("manifest.json"));
    if (!object(manifest) || manifest.artifact_type !== "agent-handoff" || ![1, 2].includes(manifest.format_version)) fail("archive");
    const checksumBytes = files.get("checksums.json");
    if (!checksumBytes && manifest.format_version !== 1) fail("checksum");
    if (checksumBytes) {
      let checksums; try { checksums = json(checksumBytes); } catch { fail("checksum"); }
      if (checksums?.algorithm !== "sha256" || !object(checksums.files)) fail("checksum");
      for (const [name, expected] of Object.entries(checksums.files)) {
        if (!files.has(name) || !/^[a-f0-9]{64}$/.test(expected) || await sha256(files.get(name)) !== expected) fail("checksum");
      }
      for (const name of files.keys()) if (name !== "checksums.json" && !Object.hasOwn(checksums.files, name)) fail("checksum");
    }
    const transcript = files.has("agent/neutral.json") ? json(files.get("agent/neutral.json")) : legacyTranscript(files.get("session.jsonl") || files.get("codex/session.jsonl"));
    if (!object(transcript) || transcript.schema !== "agent-handoff.neutral.v1" || (transcript.entries != null && !Array.isArray(transcript.entries))) fail("archive");
    transcript.entries ||= [];
    if (transcript.entries.length > limits.transcript) fail("limit");
    const optional = (name, schema) => {
      if (!files.has(name)) return null;
      const value = json(files.get(name));
      if (!object(value) || value.schema !== schema) fail("archive");
      return value;
    };
    return { manifest, transcript, checksummed: Boolean(checksumBytes), brief: optional("agent/brief.json", "agent-handoff.brief.v1"), code: optional("code/manifest.json", "agent-handoff.code.v1") };
  }

  function publicURL(raw) {
    let url; try { url = new URL(raw); } catch { fail("link"); }
    if (url.protocol !== "https:" || url.username || url.password || url.hash || url.port) fail("link");
    const host = url.hostname.toLowerCase();
    if (!host.includes(".") || host.endsWith(".localhost") || host.endsWith(".local") || host.endsWith(".internal") || host.startsWith("[") || /^\d+\.\d+\.\d+\.\d+$/.test(host)) fail("link");
    return url;
  }
  function validateRelay(manifest) {
    if (!object(manifest) || manifest.v !== 1 || !Array.isArray(manifest.r) || manifest.r.length < 1 || manifest.r.length > 3) fail("link");
    decode64(manifest.k, 32); decode64(manifest.n, 12);
    if (!/^[a-f0-9]{64}$/.test(manifest.s) || !Number.isSafeInteger(manifest.b) || manifest.b <= 0 || manifest.b > limits.encrypted) fail("link");
    const expiry = Date.parse(manifest.e);
    if (!Number.isFinite(expiry)) fail("link");
    if (expiry <= Date.now()) fail("expired");
    const seen = new Set();
    for (const replica of manifest.r) {
      if (!object(replica) || typeof replica.p !== "string" || seen.has(replica.p)) fail("link");
      seen.add(replica.p);
      const url = publicURL(replica.u), host = url.hostname;
      if (replica.p === "filebin.net") { if (host !== "filebin.net" || url.pathname.replace(/^\/+|\/+$/g, "").split("/").length !== 2) fail("link"); }
      else if (replica.p === "tmpfiles.org") { if (host !== "tmpfiles.org" || !url.pathname.startsWith("/dl/")) fail("link"); }
      else if (replica.p === "uguu.se") { if (!host.endsWith(".uguu.se")) fail("link"); }
      else if (replica.p === "temp.sh") { if (host !== "temp.sh") fail("link"); }
      else if (!/^config:[a-z0-9._-]{1,64}$/.test(replica.p)) fail("link");
    }
  }
  function linkCapability(config, href) {
    let url; try { url = new URL(href); } catch { fail("link"); }
    const params = new URLSearchParams(url.hash.slice(1));
    let descriptor, fragment;
    if (config.kind === "relay") {
      const encoded = params.get("h");
      if (!encoded || encoded.length > 21846) fail("link");
      const decoded = decode64(encoded);
      if (decoded.length > 16 * 1024) fail("link");
      try { descriptor = JSON.parse(utf8(decoded)); } catch { fail("link"); }
      validateRelay(descriptor); fragment = "h=" + encoded;
    } else {
      if (!/^[A-Za-z0-9_-]+$/.test(config.id)) fail("link");
      const key = params.get("k");
      if (typeof key !== "string" || key.length !== 43) fail("link");
      decode64(key, 32); descriptor = { k: key }; fragment = "k=" + key;
    }
    const canonical = url.origin + url.pathname + "#" + fragment;
    return { descriptor, command: 'agent-handoff import "' + canonical + '" --execute', origin: url.origin };
  }
  async function fetchBytes(fetcher, url, maximum) {
    const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), 20000);
    let reader;
    try {
      const response = await fetcher(url, { credentials: "omit", referrerPolicy: "no-referrer", redirect: "error", cache: "no-store", signal: controller.signal });
      if (!response.ok) fail(response.status === 410 ? "expired" : response.status === 404 ? "missing" : "network");
      const length = response.headers.get("content-length");
      if (length !== null && (!/^\d+$/.test(length) || Number(length) > maximum)) fail("limit");
      if (!response.body) fail("network");
      reader = response.body.getReader();
      const chunks = []; let size = 0;
      while (true) {
        const chunk = await reader.read(); if (chunk.done) break;
        if (chunk.value.length > maximum - size) fail("limit");
        chunks.push(chunk.value); size += chunk.value.length;
      }
      const bytes = new Uint8Array(size); let position = 0;
      for (const chunk of chunks) { bytes.set(chunk, position); position += chunk.length; }
      return bytes;
    } finally {
      clearTimeout(timeout);
      if (reader) { try { await reader.cancel(); } catch { /* failed fetch already reported */ } }
      controller.abort();
    }
  }
  async function decrypt(bytes, descriptor) {
    if (bytes.length !== descriptor.b || await sha256(bytes) !== descriptor.s) fail("checksum");
    const key = await crypto.subtle.importKey("raw", decode64(descriptor.k, 32), "AES-GCM", false, ["decrypt"]);
    try { return new Uint8Array(await crypto.subtle.decrypt({ name: "AES-GCM", iv: decode64(descriptor.n, 12) }, key, bytes)); } catch { fail("decrypt"); }
  }
  async function loadLink(config, href, fetcher = fetch) {
    const capability = linkCapability(config, href);
    let descriptor = capability.descriptor, replicas;
    if (config.kind === "relay") replicas = descriptor.r.map(replica => replica.u);
    else {
      let manifest;
      try { manifest = json(await fetchBytes(fetcher, capability.origin + "/v1/shares/" + config.id, 4 * 1024 * 1024)); } catch (error) { if (error.code) throw error; fail("network"); }
      if (manifest?.schema !== "agent-handoff.link.v1" || manifest.crypto?.alg !== "AES-256-GCM" || manifest.crypto?.key_ref !== "url-fragment:k") fail("link");
      descriptor = { k: descriptor.k, n: manifest.crypto.nonce, s: manifest.bundle?.sha256, b: manifest.bundle?.bytes };
      decode64(descriptor.n, 12);
      if (!Number.isSafeInteger(descriptor.b) || descriptor.b <= 0 || descriptor.b > limits.encrypted || !/^[a-f0-9]{64}$/.test(descriptor.s)) fail("link");
      // Never follow an untrusted manifest's bundle.url. The hosted service
      // contract provides this exact same-origin blob endpoint.
      replicas = [capability.origin + "/v1/shares/" + config.id + "/blob"];
    }
    for (const url of replicas) {
      try {
        const encrypted = await fetchBytes(fetcher, url, Math.min(descriptor.b, limits.encrypted));
        const bytes = await decrypt(encrypted, descriptor);
        return { bytes, command: capability.command };
      } catch (error) { if (config.kind !== "relay") { if (error.code) throw error; fail("network"); } }
    }
    fail("replicas");
  }
  return { limits, unzip, readBundle, loadLink, linkCapability };
}

// The UI uses native controls and textContent exclusively. Even a verified
// archive is untrusted: no Markdown/HTML renderer or content-derived URL exists.
async function mountReader(core, config, scope = globalThis) {
  const { document, navigator } = scope;
  const element = id => document.getElementById(id);
  const dictionary = {
    en: {
      privacy: "Decrypted and verified in your browser. The key stays in this link’s fragment; plaintext stays on this device.",
      subtitle: "Read the context here, or continue in your coding agent.", loading: "Loading and decrypting…", ready: "Ready to read", retry: "Retry", download: "Download ZIP", copy: "Copy command", copied: "Command copied", copyFailed: "Select and copy the command below.", importLabel: "Continue in an agent", fallback: "Paste this command into your terminal, or ask your Codex / Claude agent to import the full link. Import creates a new task. The ZIP contains the full handoff.",
      verified: "All bundle files passed SHA-256 verification.", legacy: "Legacy v1 bundle: file checksums are unavailable. Review with care before continuing.", summary: "At a glance", objective: "Initial request", latest_request: "Latest request", latest_update: "Latest response", notes: "Notes", conversation: "Conversation", evidence: "Tool evidence", user: "You", assistant: "Assistant", tool: "Tool", input: "Input", output: "Output", noConversation: "No visible conversation is available in this bundle. Download the ZIP for the native session.", code: "Code context", branch: "Branch", base: "Base commit", codeNote: "Files are listed for context. Nothing is executed or applied by this page.", next: "Next messages", previous: "Previous messages", truncated: "Preview shortened. The complete text is in the ZIP.", page: "Page", of: "of", messages: "messages", tools: "tool events", unknown: "Shared session",
      errors: { link: "This link is incomplete or malformed. Ask the sender for the full link, including #k= or #h=.", expired: "This share has expired. Ask the sender to create a new link.", missing: "This share is unavailable. Ask the sender for a new link.", checksum: "Integrity verification failed. No bundle content was displayed. Download only for inspection, or ask the sender to share again.", decrypt: "Decryption failed. Check that you have the complete original link, or ask the sender to share again.", archive: "This ZIP cannot be safely previewed. Download it for local inspection or use the import command.", limit: "This bundle exceeds the browser preview limits (64 MiB ZIP, 128 MiB per file, 256 MiB expanded, 16 MiB JSON, 1,024 files, 100,000 transcript records). Use the import command for local validation.", replicas: "The encrypted copies could not be read or verified. A provider may be unavailable or block browser access (CORS). Retry, or use the import command to download locally.", network: "The encrypted bundle could not be downloaded. Check your connection and retry, or use the import command.", unknown: "This browser could not open the handoff. Try a current browser or use the import command below." },
    },
    zh: {
      privacy: "解密和校验在浏览器内完成。密钥留在链接片段中，明文留在当前设备。",
      subtitle: "先阅读任务背景，也可以交给编程助手继续。", loading: "正在下载、解密和校验…", ready: "已可阅读", retry: "重试", download: "下载 ZIP", copy: "复制命令", copied: "命令已复制", copyFailed: "请选择并复制下方命令。", importLabel: "在编程助手中继续", fallback: "把命令粘贴到终端，或让 Codex / Claude 导入完整链接。导入会创建新任务；ZIP 包含完整交接内容。",
      verified: "所有文件均已通过 SHA-256 完整性校验。", legacy: "这是旧版 v1 文件，没有文件校验和。继续前请核对内容。", summary: "任务概览", objective: "最初请求", latest_request: "最新请求", latest_update: "最近回复", notes: "说明", conversation: "对话", evidence: "工具记录", user: "用户", assistant: "助手", tool: "工具", input: "输入", output: "输出", noConversation: "此包没有可显示的对话。可下载 ZIP 查看原生会话。", code: "代码背景", branch: "分支", base: "基准提交", codeNote: "文件列表仅供了解背景，本页面不会执行或应用代码。", next: "下一页消息", previous: "上一页消息", truncated: "预览已截短，完整文本保留在 ZIP 中。", page: "第", of: "/", messages: "条消息", tools: "条工具记录", unknown: "共享任务",
      errors: { link: "链接不完整或格式错误。请向发送者索取包含 #k= 或 #h= 的完整链接。", expired: "分享已过期，请让发送者重新生成链接。", missing: "分享已不可用，请让发送者重新生成链接。", checksum: "完整性校验失败，未显示包内内容。可下载用于检查，或请发送者重新分享。", decrypt: "解密失败。请核对完整原始链接，或让发送者重新分享。", archive: "无法安全预览此 ZIP。可下载后在本地检查，或使用下方导入命令。", limit: "超过浏览器预览限制（ZIP 64 MiB、单文件 128 MiB、展开后 256 MiB、JSON 16 MiB、最多 1,024 个文件和 100,000 条会话记录）。请使用导入命令在本地校验。", replicas: "无法读取或校验加密副本。服务可能不可用，或限制浏览器跨域访问（CORS）。可重试，或用下方导入命令在本地下载。", network: "加密包下载失败。请检查网络后重试，或使用下方导入命令。", unknown: "当前浏览器无法打开交接内容。请使用较新的浏览器，或用下方命令导入。" },
    },
  };
  let language = navigator.language?.toLowerCase().startsWith("zh") ? "zh" : "en";
  let result = null, errorCode = null, statusKey = "loading", page = 0, busy = false, blobURL = null;
  const t = key => dictionary[language][key] || key;
  const text = value => typeof value === "string" ? value : value && typeof value.text === "string" ? value.text : "";
  function node(tag, content, className) {
    const item = document.createElement(tag);
    if (content !== undefined) item.textContent = content;
    if (className) item.className = className;
    return item;
  }
  function block(parent, value, maximum = 32000) {
    const content = text(value);
    parent.appendChild(node("pre", content.slice(0, maximum), "text"));
    if (content.length > maximum) parent.appendChild(node("p", t("truncated"), "muted"));
  }
  function tool(parent, entry) {
    const detail = node("details", undefined, "tool");
    const name = text(entry.tool).slice(0, 180) || t("tool"), status = text(entry.status).slice(0, 80);
    detail.appendChild(node("summary", name + (status ? " · " + status : "")));
    let expanded = false;
    detail.addEventListener("toggle", () => {
      if (!detail.open || expanded) return;
      expanded = true;
      for (const field of ["input", "output"]) if (text(entry[field])) {
        detail.appendChild(node("h3", t(field))); block(detail, entry[field]);
      }
    });
    parent.appendChild(detail);
  }
  function renderBundle() {
    const content = element("content"); content.replaceChildren();
    if (!result) return;
    const entries = result.transcript.entries.filter(entry => entry && (entry.kind === "message" || entry.kind === "tool"));
    const messages = entries.filter(entry => entry.kind === "message");
    const brief = result.brief || { objective: messages.find(entry => entry.role === "user")?.text, latest_request: messages.findLast(entry => entry.role === "user")?.text, latest_update: messages.findLast(entry => entry.role === "assistant")?.text };
    const summary = node("section", undefined, "panel"); summary.appendChild(node("h2", t("summary")));
    for (const field of ["objective", "latest_request", "latest_update", "notes"]) if (text(brief[field])) {
      summary.appendChild(node("h3", t(field))); block(summary, brief[field], 4000);
    }
    if (Array.isArray(brief.evidence) && brief.evidence.length) {
      summary.appendChild(node("h3", t("evidence")));
      for (const entry of brief.evidence.slice(0, 12)) if (entry && typeof entry === "object") tool(summary, entry);
    }
    content.appendChild(summary);
    if (result.code) {
      const code = node("section", undefined, "panel"); code.appendChild(node("h2", t("code")));
      for (const [field, label] of [["branch", "branch"], ["base_commit", "base"]]) if (text(result.code[field])) code.appendChild(node("p", t(label) + ": " + text(result.code[field]).slice(0, 500)));
      code.appendChild(node("p", t("codeNote"), "muted"));
      const list = node("ul", undefined, "files");
      if (Array.isArray(result.code.files)) for (const file of result.code.files.slice(0, 1024)) {
        if (!file || typeof file.path !== "string") continue;
        list.appendChild(node("li", text(file.status).slice(0, 32) + "  " + file.path.slice(0, 1024) + (Number.isSafeInteger(file.bytes) && file.bytes >= 0 ? " · " + file.bytes.toLocaleString() + " B" : "")));
      }
      code.appendChild(list); content.appendChild(code);
    }
    const conversation = node("section", undefined, "conversation");
    conversation.appendChild(node("h2", t("conversation")));
    conversation.appendChild(node("p", messages.length.toLocaleString() + " " + t("messages") + " · " + (entries.length - messages.length).toLocaleString() + " " + t("tools"), "muted"));
    if (!entries.length) conversation.appendChild(node("p", t("noConversation")));
    const pageSize = 200, pageCount = Math.max(1, Math.ceil(entries.length / pageSize));
    page = Math.min(page, pageCount - 1);
    for (const entry of entries.slice(page * pageSize, (page + 1) * pageSize)) {
      if (entry.kind === "tool") { tool(conversation, entry); continue; }
      const message = node("article", undefined, "message " + (entry.role === "user" ? "from-user" : "from-assistant"));
      message.appendChild(node("h3", entry.role === "user" ? t("user") : t("assistant")));
      block(message, entry.text); conversation.appendChild(message);
    }
    if (pageCount > 1) {
      const navigation = node("nav", undefined, "actions"); navigation.setAttribute("aria-label", t("conversation"));
      for (const [delta, label] of [[-1, "previous"], [1, "next"]]) {
        if (page + delta < 0 || page + delta >= pageCount) continue;
        const button = node("button", t(label)); button.type = "button";
        button.addEventListener("click", () => { page += delta; renderBundle(); element("content").focus(); });
        navigation.appendChild(button);
      }
      navigation.appendChild(node("span", t("page") + " " + (page + 1) + " " + t("of") + " " + pageCount));
      conversation.appendChild(navigation);
    }
    content.appendChild(conversation);
  }
  function render() {
    document.documentElement.lang = language === "zh" ? "zh-CN" : "en";
    element("language").value = language;
    for (const [id, key] of [["privacy", "privacy"], ["subtitle", "subtitle"], ["retry", "retry"], ["download", "download"], ["copy", "copy"], ["import-label", "importLabel"], ["fallback", "fallback"]]) element(id).textContent = t(key);
    element("command").setAttribute("aria-label", t("importLabel"));
    element("status").textContent = errorCode ? "" : t(statusKey);
    element("error").hidden = !errorCode;
    element("error").textContent = errorCode ? dictionary[language].errors[errorCode] || dictionary[language].errors.unknown : "";
    element("retry").disabled = busy;
    element("copy").disabled = !element("command").value;
    element("verified").hidden = !result;
    element("verified").textContent = result ? t(result.checksummed ? "verified" : "legacy") : "";
    if (result) {
      const title = typeof result.manifest.title === "string" && result.manifest.title.trim() ? result.manifest.title.slice(0, 300) : t("unknown");
      document.querySelector("h1").textContent = title; document.title = title + " — Agent Handoff";
    }
    renderBundle();
  }
  async function load() {
    if (busy) return;
    busy = true; errorCode = null; result = null; page = 0; statusKey = "loading"; render();
    try {
      const capability = core.linkCapability(config, scope.location.href);
      element("command").value = capability.command;
      const loaded = await core.loadLink(config, scope.location.href, scope.fetch.bind(scope));
      if (blobURL) scope.URL.revokeObjectURL(blobURL);
      blobURL = scope.URL.createObjectURL(new scope.Blob([loaded.bytes], { type: "application/zip" }));
      const download = element("download"); download.href = blobURL; download.download = "task.agent-handoff.zip"; download.disabled = false; download.setAttribute("aria-disabled", "false"); download.tabIndex = 0;
      result = await core.readBundle(loaded.bytes); statusKey = "ready";
    } catch (error) { errorCode = error.code || "unknown"; }
    busy = false; render();
  }
  element("language").addEventListener("change", () => { language = element("language").value === "zh" ? "zh" : "en"; render(); });
  element("retry").addEventListener("click", load);
  element("copy").addEventListener("click", async () => {
    try { await navigator.clipboard.writeText(element("command").value); statusKey = "copied"; }
    catch { element("command").focus(); element("command").select(); statusKey = "copyFailed"; }
    element("status").textContent = t(statusKey);
  });
  element("download").addEventListener("click", event => { if (!blobURL) event.preventDefault(); });
  scope.addEventListener("pagehide", () => { if (blobURL) scope.URL.revokeObjectURL(blobURL); });
  await load();
}`;

export function readerPageResponse(config, title, escapeHTML) {
  const nonce = Array.from(crypto.getRandomValues(new Uint8Array(18)), byte => byte.toString(16).padStart(2, "0")).join("");
  const script = browserReaderSource + "\nmountReader(createReaderCore(), " + JSON.stringify(config).replace(/</g, "\\u003c") + ");";
  const html = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHTML(title)} — Agent Handoff</title>
<style nonce="${nonce}">
:root{color-scheme:light dark;--border:#8885;--muted:#666;--paper:#fff;--ink:#242424;--accent:#1769e0}*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font:15px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}main{max-width:860px;margin:40px auto 64px;padding:0 24px}.top{display:flex;align-items:center;justify-content:space-between;gap:16px}.brand{font-size:11px;font-weight:650;letter-spacing:.12em;color:var(--muted)}h1{font-size:clamp(1.5rem,4vw,2rem);line-height:1.3;margin:22px 0 8px;overflow-wrap:anywhere}h2{font-size:1.1rem;margin:0 0 12px}h3{font-size:.83rem;margin:16px 0 6px;color:var(--muted)}p{margin:10px 0}.muted,#privacy{color:var(--muted);font-size:.86rem}.panel{border:1px solid var(--border);border-radius:8px;padding:20px;margin:24px 0}.actions{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin:16px 0}button,.button,select{font:inherit;border:1px solid var(--border);border-radius:6px;background:transparent;color:inherit;padding:7px 12px}button,.button,summary{cursor:pointer}.button{text-decoration:none;display:inline-block}.button[aria-disabled="true"],button:disabled{opacity:.45;cursor:default}button:hover,.button:hover{background:#8881}:focus-visible{outline:3px solid var(--accent);outline-offset:3px}textarea{display:block;resize:vertical;width:100%;min-height:76px;border:1px solid var(--border);border-radius:6px;padding:10px;background:transparent;color:inherit;font:12px/1.5 ui-monospace,SFMono-Regular,Consolas,monospace;overflow-wrap:anywhere}#error{border-left:3px solid #b34229;padding:10px 14px;background:#b3422910}#verified{font-size:.84rem;color:var(--muted)}.text{font:inherit;white-space:pre-wrap;overflow-wrap:anywhere;margin:0}.message{padding:16px 18px;border-left:2px solid var(--border);margin:20px 0}.message h3{margin:0 0 8px}.from-user{background:#88808;border-left-color:var(--accent)}.tool{border:1px solid var(--border);border-radius:6px;margin:12px 0;padding:10px 14px}.tool summary{overflow-wrap:anywhere;font-size:.88rem}.tool .text{font:12px/1.6 ui-monospace,SFMono-Regular,Consolas,monospace;max-height:420px;overflow:auto}.files{padding-left:22px;max-height:300px;overflow:auto;font:12px/1.8 ui-monospace,SFMono-Regular,Consolas,monospace;overflow-wrap:anywhere}.conversation{margin-top:32px}.install{margin-top:32px;border-top:1px solid var(--border);padding-top:20px}.install pre{white-space:pre-wrap;overflow-wrap:anywhere;font-size:12px}.install a{color:var(--accent)}[hidden]{display:none!important}@media(prefers-color-scheme:dark){:root{--paper:#181818;--ink:#eee;--muted:#aaa}}@media(max-width:560px){main{margin:24px auto 40px;padding:0 16px}.panel{padding:16px}.message{padding:12px}.actions{gap:8px}}
</style></head><body><main>
<header><div class="top"><span class="brand">AGENT HANDOFF</span><label><span class="brand">Language / 语言 </span><select id="language" aria-label="Language / 语言"><option value="en">English</option><option value="zh">简体中文</option></select></label></div><h1>${escapeHTML(title)}</h1><p id="subtitle">Read the context here, or continue in your coding agent.</p><p id="privacy">Decrypted and verified in your browser. The key stays in the URL fragment.</p></header>
<div class="actions"><a id="download" class="button" aria-disabled="true" tabindex="-1">Download ZIP</a><button id="retry" type="button">Retry</button><span id="status" role="status" aria-live="polite">Loading…</span></div>
<p id="error" role="alert" hidden></p><p id="verified" hidden></p>
<section class="panel" aria-labelledby="import-label"><h2 id="import-label">Continue in an agent</h2><textarea id="command" readonly spellcheck="false" aria-label="Import command"></textarea><div class="actions"><button id="copy" type="button">Copy command</button></div><p id="fallback" class="muted">Use the full link, including #k= or #h=. Import creates a new task.</p></section>
<section id="content" tabindex="-1" aria-label="Shared task / 共享任务"></section>
<details class="install"><summary>First time using Agent Handoff? / 首次使用？</summary><p>Install the plugin, restart your agent, then ask it to import this full link.<br>安装插件并重启编程助手，再让它导入完整链接。</p><pre><code># Claude Code
claude plugin marketplace add https://github.com/DavidDingXu/agent-handoff
claude plugin install agent-handoff@agent-handoff

# Codex
codex plugin marketplace add https://github.com/DavidDingXu/agent-handoff
codex plugin add agent-handoff@agent-handoff</code></pre><a href="https://github.com/DavidDingXu/agent-handoff" rel="noreferrer">Source code and documentation / 源码与文档</a></details>
<noscript><p>JavaScript is required for local decryption. Import the complete link with agent-handoff instead.<br>本地解密需要 JavaScript，也可用 agent-handoff 导入完整链接。</p></noscript>
</main><script nonce="${nonce}">${script}</script></body></html>`;
  return new Response(html, { headers: {
    "content-type": "text/html; charset=utf-8", "cache-control": "no-store", "referrer-policy": "no-referrer", "x-content-type-options": "nosniff",
    "content-security-policy": `default-src 'none'; connect-src 'self' https:; script-src 'nonce-${nonce}'; style-src 'nonce-${nonce}'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; object-src 'none'`,
  } });
}
