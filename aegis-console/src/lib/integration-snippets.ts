import type { SecurityLevel } from "@/lib/api/app-auth-protocol";
import type { CodeLanguageId } from "@/lib/code-languages";

/**
 * 接入示例生成器。
 *
 * 控制台「接入」页与公开开发者门户共用这一份数据，避免两处内容漂移。
 * 新增语言时在对应场景的数组里加一项，UI 会自动多出一个 Tab。
 *
 * 与服务端的对应关系（改协议时必须同步，否则接入自检会失败）：
 *   待签名字符串     → internal/service/auth_protocol_service.go computeRequestSignature
 *   Transport v2 封包 → internal/service/auth_protocol_selftest.go sealSelfTestPayload
 */

export type Snippet = { lang: CodeLanguageId; code: string };

export type Scenario = {
  id: string;
  label: string;
  /** 一句话说明这个场景在做什么，显示在代码块上方 */
  summary: string;
  snippets: Snippet[];
};

export type SnippetContext = {
  baseUrl: string;
  appKey: string;
  level: SecurityLevel;
};

const SECRET = "sk_你的应用密钥";

/** 待签名字符串模板，signed / sealed 两档共用，注释里逐字节列出 */
const CANONICAL = `aegis-hmac-sha256
{appKey}
{大写 HTTP 方法}
{请求路径，不含 query}
{原样 query string，不含 ?；没有 query 时是空行，不能省}
{Unix 秒级时间戳}
{随机 nonce}
{sha256Hex(请求体)}`;

// ─────────────────────────────────────────────────────────────────────
// 场景 1：登录。三档安全等级里唯一有差异的一段。
// ─────────────────────────────────────────────────────────────────────

function standardLogin({ baseUrl, appKey }: SnippetContext): Snippet[] {
  const url = `${baseUrl}/api/v1/apps/${appKey}/auth/login`;
  return [
    {
      lang: "curl",
      code: `curl -X POST "${url}" \\
  -H "Content-Type: application/json" \\
  -d '{"account":"alice","password":"secret"}'`
    },
    {
      lang: "typescript",
      code: `const BASE = "${baseUrl}";
const APP_KEY = "${appKey}";

export async function login(account: string, password: string) {
  const res = await fetch(\`\${BASE}/api/v1/apps/\${APP_KEY}/auth/login\`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ account, password })
  });
  const { code, message, data } = await res.json();
  if (code !== 200) throw new Error(message);
  return data; // { accessToken, refreshToken, expiresAt, userId, account }
}`
    },
    {
      lang: "python",
      code: `import requests

BASE = "${baseUrl}"
APP_KEY = "${appKey}"

def login(account: str, password: str) -> dict:
    res = requests.post(
        f"{BASE}/api/v1/apps/{APP_KEY}/auth/login",
        json={"account": account, "password": password},
        timeout=10,
    )
    envelope = res.json()
    if envelope["code"] != 200:
        raise RuntimeError(envelope["message"])
    return envelope["data"]`
    },
    {
      lang: "go",
      code: `const (
    base   = "${baseUrl}"
    appKey = "${appKey}"
)

func Login(ctx context.Context, account, password string) (map[string]any, error) {
    payload, _ := json.Marshal(map[string]string{"account": account, "password": password})
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
        base+"/api/v1/apps/"+appKey+"/auth/login", bytes.NewReader(payload))
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var envelope struct {
        Code    int            \`json:"code"\`
        Message string         \`json:"message"\`
        Data    map[string]any \`json:"data"\`
    }
    if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
        return nil, err
    }
    if envelope.Code != 200 {
        return nil, errors.New(envelope.Message)
    }
    return envelope.Data, nil
}`
    },
    {
      lang: "java",
      code: `private static final String BASE = "${baseUrl}";
private static final String APP_KEY = "${appKey}";

public JsonNode login(String account, String password) throws Exception {
    ObjectMapper mapper = new ObjectMapper();
    String body = mapper.writeValueAsString(Map.of("account", account, "password", password));

    HttpRequest request = HttpRequest.newBuilder()
            .uri(URI.create(BASE + "/api/v1/apps/" + APP_KEY + "/auth/login"))
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(body))
            .build();

    HttpResponse<String> response = HttpClient.newHttpClient()
            .send(request, HttpResponse.BodyHandlers.ofString());
    JsonNode envelope = mapper.readTree(response.body());
    if (envelope.get("code").asInt() != 200) {
        throw new IllegalStateException(envelope.get("message").asText());
    }
    return envelope.get("data");
}`
    },
    {
      lang: "kotlin",
      code: `private const val BASE = "${baseUrl}"
private const val APP_KEY = "${appKey}"

suspend fun login(account: String, password: String): JSONObject = withContext(Dispatchers.IO) {
    val body = JSONObject().put("account", account).put("password", password)
    val request = Request.Builder()
        .url("$BASE/api/v1/apps/$APP_KEY/auth/login")
        .post(body.toString().toRequestBody("application/json".toMediaType()))
        .build()

    OkHttpClient().newCall(request).execute().use { response ->
        val envelope = JSONObject(response.body!!.string())
        check(envelope.getInt("code") == 200) { envelope.getString("message") }
        envelope.getJSONObject("data")
    }
}`
    },
    {
      lang: "swift",
      code: `let base = "${baseUrl}"
let appKey = "${appKey}"

func login(account: String, password: String) async throws -> [String: Any] {
    var request = URLRequest(url: URL(string: "\\(base)/api/v1/apps/\\(appKey)/auth/login")!)
    request.httpMethod = "POST"
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    request.httpBody = try JSONSerialization.data(
        withJSONObject: ["account": account, "password": password])

    let (data, _) = try await URLSession.shared.data(for: request)
    let envelope = try JSONSerialization.jsonObject(with: data) as! [String: Any]
    guard envelope["code"] as? Int == 200 else {
        throw NSError(domain: "Aegis", code: envelope["code"] as? Int ?? -1,
                      userInfo: [NSLocalizedDescriptionKey: envelope["message"] as? String ?? ""])
    }
    return envelope["data"] as! [String: Any]
}`
    },
    {
      lang: "php",
      code: `<?php
const AEGIS_BASE = '${baseUrl}';
const AEGIS_APP_KEY = '${appKey}';

function aegis_login(string $account, string $password): array {
    $ch = curl_init(AEGIS_BASE . '/api/v1/apps/' . AEGIS_APP_KEY . '/auth/login');
    curl_setopt_array($ch, [
        CURLOPT_POST => true,
        CURLOPT_POSTFIELDS => json_encode(['account' => $account, 'password' => $password]),
        CURLOPT_HTTPHEADER => ['Content-Type: application/json'],
        CURLOPT_RETURNTRANSFER => true,
    ]);
    $envelope = json_decode(curl_exec($ch), true);
    curl_close($ch);

    if (($envelope['code'] ?? 0) !== 200) {
        throw new RuntimeException($envelope['message'] ?? '登录失败');
    }
    return $envelope['data'];
}`
    },
    {
      lang: "csharp",
      code: `const string Base = "${baseUrl}";
const string AppKey = "${appKey}";

static readonly HttpClient Http = new();

public static async Task<JsonElement> LoginAsync(string account, string password)
{
    var payload = JsonSerializer.Serialize(new { account, password });
    using var content = new StringContent(payload, Encoding.UTF8, "application/json");

    var response = await Http.PostAsync($"{Base}/api/v1/apps/{AppKey}/auth/login", content);
    var envelope = JsonDocument.Parse(await response.Content.ReadAsStringAsync()).RootElement;

    if (envelope.GetProperty("code").GetInt32() != 200)
        throw new InvalidOperationException(envelope.GetProperty("message").GetString());

    return envelope.GetProperty("data");
}`
    },
    {
      lang: "ruby",
      code: `require "net/http"
require "json"

BASE = "${baseUrl}"
APP_KEY = "${appKey}"

def login(account, password)
  uri = URI("#{BASE}/api/v1/apps/#{APP_KEY}/auth/login")
  res = Net::HTTP.post(uri, { account: account, password: password }.to_json,
                       "Content-Type" => "application/json")
  envelope = JSON.parse(res.body)
  raise envelope["message"] unless envelope["code"] == 200

  envelope["data"]
end`
    },
    {
      lang: "dart",
      code: `import 'dart:convert';
import 'package:http/http.dart' as http;

const base = '${baseUrl}';
const appKey = '${appKey}';

Future<Map<String, dynamic>> login(String account, String password) async {
  final res = await http.post(
    Uri.parse('$base/api/v1/apps/$appKey/auth/login'),
    headers: {'Content-Type': 'application/json'},
    body: jsonEncode({'account': account, 'password': password}),
  );
  final envelope = jsonDecode(res.body) as Map<String, dynamic>;
  if (envelope['code'] != 200) throw Exception(envelope['message']);
  return envelope['data'] as Map<String, dynamic>;
}`
    }
  ];
}

function signedLogin({ baseUrl, appKey }: SnippetContext): Snippet[] {
  const path = `/api/v1/apps/${appKey}/auth/login`;
  return [
    {
      lang: "curl",
      code: `APP_KEY="${appKey}"
SECRET="${SECRET}"
REQ_PATH="${path}"
BODY='{"account":"alice","password":"secret"}'
TS=$(date +%s)
NONCE=$(uuidgen)
BODY_HASH=$(printf '%s' "$BODY" | openssl dgst -sha256 -hex | awk '{print $2}')

# 换行必须是真正的 \\n，字段顺序不能变。
# 第 5 行是原样 query string；这个请求没有 query，所以留一个空行 —— 不能省
CANONICAL=$(printf 'aegis-hmac-sha256\\n%s\\nPOST\\n%s\\n\\n%s\\n%s\\n%s' \\
  "$APP_KEY" "$REQ_PATH" "$TS" "$NONCE" "$BODY_HASH")
SIG=$(printf '%s' "$CANONICAL" | openssl dgst -sha256 -hmac "$SECRET" -hex | awk '{print $2}')

curl -X POST "${baseUrl}$REQ_PATH" \\
  -H "Content-Type: application/json" \\
  -H "X-Aegis-App-Key: $APP_KEY" \\
  -H "X-Aegis-Timestamp: $TS" \\
  -H "X-Aegis-Nonce: $NONCE" \\
  -H "X-Aegis-Signature: v2=$SIG" \\
  -d "$BODY"`
    },
    {
      lang: "typescript",
      code: `const BASE = "${baseUrl}";
const APP_KEY = "${appKey}";
const SECRET = "${SECRET}"; // 只放服务端，不要打进客户端包

const hex = (buffer: ArrayBuffer) =>
  [...new Uint8Array(buffer)].map((b) => b.toString(16).padStart(2, "0")).join("");

/* 待签名字符串：
${CANONICAL.split("\n").map((line) => `   ${line}`).join("\n")}
*/
// query 是原样的查询串（不含 ?），没有就传空串 —— 那一行必须留着
async function sign(method: string, path: string, query: string, body: string) {
  const enc = new TextEncoder();
  const timestamp = Math.floor(Date.now() / 1000).toString();
  const nonce = crypto.randomUUID();
  const bodyHash = hex(await crypto.subtle.digest("SHA-256", enc.encode(body)));
  const canonical =
    ["aegis-hmac-sha256", APP_KEY, method, path, query, timestamp, nonce, bodyHash].join("\\n");

  const key = await crypto.subtle.importKey(
    "raw", enc.encode(SECRET), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const signature = hex(await crypto.subtle.sign("HMAC", key, enc.encode(canonical)));

  return {
    "X-Aegis-App-Key": APP_KEY,
    "X-Aegis-Timestamp": timestamp,
    "X-Aegis-Nonce": nonce,
    "X-Aegis-Signature": \`v2=\${signature}\`
  };
}

// 业务调用与标准档相同，只是多了一次 sign()
export async function login(account: string, password: string) {
  const path = \`/api/v1/apps/\${APP_KEY}/auth/login\`;
  const body = JSON.stringify({ account, password });
  const res = await fetch(BASE + path, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...(await sign("POST", path, "", body)) },
    body
  });
  const { code, message, data } = await res.json();
  if (code !== 200) throw new Error(message);
  return data;
}`
    },
    {
      lang: "python",
      code: `import hashlib, hmac, json, time, uuid, requests

BASE = "${baseUrl}"
APP_KEY = "${appKey}"
SECRET = "${SECRET}"

def sign_headers(method: str, path: str, body: str, query: str = "") -> dict:
    timestamp = str(int(time.time()))
    nonce = str(uuid.uuid4())
    body_hash = hashlib.sha256(body.encode()).hexdigest()
    # query 原样参与，没有就是空串 —— 那一行必须留着
    canonical = "\\n".join(
        ["aegis-hmac-sha256", APP_KEY, method, path, query, timestamp, nonce, body_hash]
    )
    signature = hmac.new(SECRET.encode(), canonical.encode(), hashlib.sha256).hexdigest()
    return {
        "X-Aegis-App-Key": APP_KEY,
        "X-Aegis-Timestamp": timestamp,
        "X-Aegis-Nonce": nonce,
        "X-Aegis-Signature": f"v2={signature}",
    }

def login(account: str, password: str) -> dict:
    path = f"/api/v1/apps/{APP_KEY}/auth/login"
    body = json.dumps({"account": account, "password": password})
    headers = {"Content-Type": "application/json", **sign_headers("POST", path, body)}

    envelope = requests.post(BASE + path, data=body, headers=headers, timeout=10).json()
    if envelope["code"] != 200:
        raise RuntimeError(envelope["message"])
    return envelope["data"]`
    },
    {
      lang: "go",
      code: `const (
    base   = "${baseUrl}"
    appKey = "${appKey}"
    secret = "${SECRET}"
)

// query 是原样的查询串（不含 ?），没有就传空串 —— 那一行必须留着
func signHeaders(method, path, query string, body []byte) map[string]string {
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)
    nonce := uuid.NewString()
    bodyHash := sha256.Sum256(body)
    canonical := strings.Join([]string{
        "aegis-hmac-sha256", appKey, method, path, query,
        timestamp, nonce, hex.EncodeToString(bodyHash[:]),
    }, "\\n")

    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(canonical))
    return map[string]string{
        "X-Aegis-App-Key":   appKey,
        "X-Aegis-Timestamp": timestamp,
        "X-Aegis-Nonce":     nonce,
        "X-Aegis-Signature": "v2=" + hex.EncodeToString(mac.Sum(nil)),
    }
}`
    },
    {
      lang: "java",
      code: `private static final String APP_KEY = "${appKey}";
private static final String SECRET = "${SECRET}";

private static String hex(byte[] bytes) {
    StringBuilder sb = new StringBuilder(bytes.length * 2);
    for (byte b : bytes) sb.append(String.format("%02x", b));
    return sb.toString();
}

// query 是原样的查询串（不含 ?），没有就传空串 —— 那一行必须留着
Map<String, String> signHeaders(String method, String path, String query, String body)
        throws Exception {
    String timestamp = String.valueOf(Instant.now().getEpochSecond());
    String nonce = UUID.randomUUID().toString();
    String bodyHash = hex(MessageDigest.getInstance("SHA-256").digest(body.getBytes(UTF_8)));
    String canonical = String.join("\\n",
            "aegis-hmac-sha256", APP_KEY, method, path, query, timestamp, nonce, bodyHash);

    Mac mac = Mac.getInstance("HmacSHA256");
    mac.init(new SecretKeySpec(SECRET.getBytes(UTF_8), "HmacSHA256"));

    return Map.of(
            "X-Aegis-App-Key", APP_KEY,
            "X-Aegis-Timestamp", timestamp,
            "X-Aegis-Nonce", nonce,
            "X-Aegis-Signature", "v2=" + hex(mac.doFinal(canonical.getBytes(UTF_8))));
}`
    },
    {
      lang: "kotlin",
      code: `private const val APP_KEY = "${appKey}"
private const val SECRET = "${SECRET}"

private fun ByteArray.hex() = joinToString("") { "%02x".format(it) }

// query 是原样的查询串（不含 ?），没有就传空串 —— 那一行必须留着
fun signHeaders(
    method: String, path: String, body: String, query: String = ""
): Map<String, String> {
    val timestamp = (System.currentTimeMillis() / 1000).toString()
    val nonce = UUID.randomUUID().toString()
    val bodyHash = MessageDigest.getInstance("SHA-256").digest(body.toByteArray()).hex()
    val canonical = listOf(
        "aegis-hmac-sha256", APP_KEY, method, path, query, timestamp, nonce, bodyHash
    ).joinToString("\\n")

    val mac = Mac.getInstance("HmacSHA256")
    mac.init(SecretKeySpec(SECRET.toByteArray(), "HmacSHA256"))
    return mapOf(
        "X-Aegis-App-Key" to APP_KEY,
        "X-Aegis-Timestamp" to timestamp,
        "X-Aegis-Nonce" to nonce,
        "X-Aegis-Signature" to "v2=" + mac.doFinal(canonical.toByteArray()).hex()
    )
}`
    },
    {
      lang: "php",
      code: `<?php
const AEGIS_APP_KEY = '${appKey}';
const AEGIS_SECRET = '${SECRET}';

function aegis_sign_headers(string $method, string $path, string $body, string $query = ''): array {
    $timestamp = (string) time();
    $nonce = bin2hex(random_bytes(16));
    $bodyHash = hash('sha256', $body);
    // query 原样参与，没有就是空串 —— 那一行必须留着
    $canonical = implode("\\n", [
        'aegis-hmac-sha256', AEGIS_APP_KEY, $method, $path, $query, $timestamp, $nonce, $bodyHash,
    ]);
    $signature = hash_hmac('sha256', $canonical, AEGIS_SECRET);

    return [
        'X-Aegis-App-Key: ' . AEGIS_APP_KEY,
        'X-Aegis-Timestamp: ' . $timestamp,
        'X-Aegis-Nonce: ' . $nonce,
        'X-Aegis-Signature: v2=' . $signature,
    ];
}`
    },
    {
      lang: "csharp",
      code: `const string AppKey = "${appKey}";
const string Secret = "${SECRET}";

// query 是原样的查询串（不含 ?），没有就传空串 —— 那一行必须留着
static Dictionary<string, string> SignHeaders(string method, string path, string body, string query = "")
{
    var timestamp = DateTimeOffset.UtcNow.ToUnixTimeSeconds().ToString();
    var nonce = Guid.NewGuid().ToString();
    var bodyHash = Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(body))).ToLowerInvariant();
    var canonical = string.Join("\\n",
        "aegis-hmac-sha256", AppKey, method, path, query, timestamp, nonce, bodyHash);

    using var mac = new HMACSHA256(Encoding.UTF8.GetBytes(Secret));
    var signature = Convert.ToHexString(mac.ComputeHash(Encoding.UTF8.GetBytes(canonical)))
        .ToLowerInvariant();

    return new Dictionary<string, string>
    {
        ["X-Aegis-App-Key"] = AppKey,
        ["X-Aegis-Timestamp"] = timestamp,
        ["X-Aegis-Nonce"] = nonce,
        ["X-Aegis-Signature"] = $"v2={signature}",
    };
}`
    }
  ];
}

function sealedLogin({ baseUrl, appKey }: SnippetContext): Snippet[] {
  return [
    {
      lang: "typescript",
      code: `import { xchacha20poly1305 } from "@noble/ciphers/chacha";
import { x25519 } from "@noble/curves/ed25519";
import { hkdf } from "@noble/hashes/hkdf";
import { sha256 } from "@noble/hashes/sha2";

const BASE = "${baseUrl}";
const APP_KEY = "${appKey}";
const SECRET = "${SECRET}";
const enc = new TextEncoder();

const b64u = {
  encode: (b: Uint8Array) =>
    btoa(String.fromCharCode(...b)).replace(/\\+/g, "-").replace(/\\//g, "_").replace(/=+$/, ""),
  decode: (t: string) => {
    const s = t.replace(/-/g, "+").replace(/_/g, "/");
    return Uint8Array.from(atob(s + "=".repeat((4 - (s.length % 4)) % 4)), (c) => c.charCodeAt(0));
  }
};

// 顺序固定：先加密出密文，再对该密文签名。
export async function callSealed<T>(path: string, payload: unknown): Promise<T> {
  const config = await (await fetch(\`\${BASE}/api/v1/apps/\${APP_KEY}/config\`)).json();
  const transport = config.data.security.transport;
  const keyId: string = transport.activeKeyId;
  const serverKey = transport.publicKeys.find((k: { keyId: string }) => k.keyId === keyId);

  const clientPrivate = x25519.utils.randomPrivateKey();
  const nonce = crypto.getRandomValues(new Uint8Array(24));
  const nonceB64 = b64u.encode(nonce);
  const timestamp = Math.floor(Date.now() / 1000).toString();

  // 请求 AAD：7 行，\\n 分隔，末尾无换行
  const aad = enc.encode(
    ["aegis-transport-v2", APP_KEY, keyId, "POST", path, timestamp, nonceB64].join("\\n")
  );
  const shared = x25519.getSharedSecret(clientPrivate, b64u.decode(serverKey.publicKey));
  // HKDF 盐取自公开的 appKey，客户端不需要知道任何内部 ID
  const salt = sha256(enc.encode(\`\${APP_KEY}:\${keyId}\`));
  const requestKey = hkdf(sha256, shared, salt, aad, 32);

  const ciphertext = b64u.encode(
    xchacha20poly1305(requestKey, nonce, aad).encrypt(enc.encode(JSON.stringify(payload)))
  );

  const res = await fetch(BASE + path, {
    method: "POST",
    headers: {
      "Content-Type": "application/octet-stream",
      "X-Aegis-Protocol": "aegis-transport-v2",
      "X-Aegis-App-Key": APP_KEY,
      "X-Aegis-Key-Id": keyId,
      "X-Aegis-Client-Key": b64u.encode(x25519.getPublicKey(clientPrivate)),
      "X-Aegis-Timestamp": timestamp,
      "X-Aegis-Nonce": nonceB64,
      "X-Aegis-Signature": await signCiphertext("POST", path, timestamp, nonceB64, ciphertext)
    },
    body: ciphertext
  });

  // 响应换一把密钥、换一种 AAD 格式（6 行，第 4 行是 HTTP 状态码）
  const responseNonceB64 = res.headers.get("X-Aegis-Response-Nonce")!;
  const responseKey = sha256(concat(requestKey, enc.encode("aegis-response-v2")));
  const responseAad = enc.encode(
    ["aegis-transport-v2", APP_KEY, keyId, String(res.status), nonceB64, responseNonceB64].join("\\n")
  );
  const plaintext = xchacha20poly1305(responseKey, b64u.decode(responseNonceB64), responseAad)
    .decrypt(b64u.decode(await res.text()));

  return JSON.parse(new TextDecoder().decode(plaintext)) as T;
}

function concat(a: Uint8Array, b: Uint8Array) {
  const out = new Uint8Array(a.length + b.length);
  out.set(a);
  out.set(b, a.length);
  return out;
}

// 签名规则与签名档相同，只是 body 换成了密文
async function signCiphertext(
  method: string, path: string, timestamp: string, nonce: string, ciphertext: string
) {
  const hex = (buf: ArrayBuffer) =>
    [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, "0")).join("");
  const bodyHash = hex(await crypto.subtle.digest("SHA-256", enc.encode(ciphertext)));
  // 第 5 行是原样 query。POST 的密文走 body，所以这里是空行；
  // 无请求体的方法（GET/DELETE）把密文放进 ?_payload=，那一行就是 "_payload=<密文>"
  const canonical =
    ["aegis-hmac-sha256", APP_KEY, method, path, "", timestamp, nonce, bodyHash].join("\\n");
  const key = await crypto.subtle.importKey(
    "raw", enc.encode(SECRET), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return \`v2=\${hex(await crypto.subtle.sign("HMAC", key, enc.encode(canonical)))}\`;
}`
    },
    {
      lang: "go",
      code: `// 依赖：golang.org/x/crypto/{chacha20poly1305,hkdf}，crypto/ecdh 是标准库
const (
    base   = "${baseUrl}"
    appKey = "${appKey}"
)

func sealPayload(keyID, serverPublicB64, path, timestamp string, payload []byte) (
    body []byte, requestKey []byte, nonceB64, clientPubB64 string, err error,
) {
    serverPub, err := base64.RawURLEncoding.DecodeString(serverPublicB64)
    if err != nil {
        return nil, nil, "", "", err
    }
    curve := ecdh.X25519()
    clientPriv, err := curve.GenerateKey(rand.Reader)
    if err != nil {
        return nil, nil, "", "", err
    }
    peer, err := curve.NewPublicKey(serverPub)
    if err != nil {
        return nil, nil, "", "", err
    }
    shared, err := clientPriv.ECDH(peer)
    if err != nil {
        return nil, nil, "", "", err
    }

    nonce := make([]byte, chacha20poly1305.NonceSizeX)
    if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, nil, "", "", err
    }
    nonceB64 = base64.RawURLEncoding.EncodeToString(nonce)

    // 请求 AAD：7 行，\\n 分隔，末尾无换行
    aad := []byte(strings.Join([]string{
        "aegis-transport-v2", appKey, keyID, http.MethodPost, path, timestamp, nonceB64,
    }, "\\n"))

    // HKDF 盐 = SHA-256("{appKey}:{keyId}")，info = 上面的 AAD
    salt := sha256.Sum256([]byte(appKey + ":" + keyID))
    requestKey = make([]byte, chacha20poly1305.KeySize)
    if _, err = io.ReadFull(hkdf.New(sha256.New, shared, salt[:], aad), requestKey); err != nil {
        return nil, nil, "", "", err
    }

    aead, err := chacha20poly1305.NewX(requestKey)
    if err != nil {
        return nil, nil, "", "", err
    }
    sealed := aead.Seal(nil, nonce, payload, aad)
    return []byte(base64.RawURLEncoding.EncodeToString(sealed)), requestKey, nonceB64,
        base64.RawURLEncoding.EncodeToString(clientPriv.PublicKey().Bytes()), nil
}`
    },
    {
      lang: "kotlin",
      code: `// 依赖：OkHttp + BouncyCastle(bcprov-jdk18on)。
// 这一层是**三档通用**的：standard 只补 App-Key 头，signed 追加签名，
// sealed 再叠一层加密 —— 业务代码一行不动，换档只换这个拦截器的配置。
// 完整可用版本见仓库 sdk/kotlin，下面是最小骨架。
class AegisInterceptor(
    private val appKey: String,
    private val appSecret: String?,   // 只在 signed / sealed 档需要，且只能放服务端
    private val config: () -> AegisConfig,
) : Interceptor {

    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        // /config 与 OAuth 回跳在任何等级下都免包装
        val path = request.url.encodedPath
        if (path.endsWith("/config") || path.endsWith("/auth/oauth/callback")) {
            return chain.proceed(request)
        }
        val withKey = request.newBuilder().header("X-Aegis-App-Key", appKey).build()
        return when (config().security.level) {
            "sealed" -> proceedSealed(chain, withKey)
            "signed" -> chain.proceed(sign(withKey, nonce = null))
            else -> chain.proceed(withKey)
        }
    }

    /** signed：v2 待签名字符串共 8 行，第 5 行是原样 query（没有就是空行）。 */
    private fun sign(request: Request, nonce: String?): Request {
        val secret = requireNotNull(appSecret) { "signed / sealed 档必须提供 appSecret" }
        val timestamp = (System.currentTimeMillis() / 1000).toString()
        val actualNonce = nonce ?: b64u(random(16))
        val body = Buffer().also { request.body?.writeTo(it) }.readByteArray()
        val canonical = listOf(
            "aegis-hmac-sha256", appKey, request.method.uppercase(),
            request.url.encodedPath, request.url.encodedQuery.orEmpty(),
            timestamp, actualNonce, sha256Hex(body),
        ).joinToString("\\n")
        val mac = Mac.getInstance("HmacSHA256").apply {
            init(SecretKeySpec(secret.toByteArray(), "HmacSHA256"))
        }
        return request.newBuilder()
            .header("X-Aegis-Timestamp", timestamp)
            .header("X-Aegis-Nonce", actualNonce)
            .header("X-Aegis-Signature", "v2=" + mac.doFinal(canonical.toByteArray()).hex())
            .build()
    }

    /** sealed：加密载荷 → 对密文签名 → 解开响应。 */
    private fun proceedSealed(chain: Interceptor.Chain, request: Request): Response {
        val spec = config().security.transport!!
        val serverKey = spec.publicKeys.first { it.keyId == spec.activeKeyId }
        val ephemeral = x25519KeyPair()
        val shared = x25519(ephemeral.private, b64uDecode(serverKey.publicKey))
        val nonce = random(24)
        val nonceB64 = b64u(nonce)
        val timestamp = (System.currentTimeMillis() / 1000).toString()

        // 请求 AAD 七行，**不含 query**；HKDF 的 info 就是它
        val aad = listOf(
            "aegis-transport-v2", appKey, serverKey.keyId,
            request.method.uppercase(), request.url.encodedPath, timestamp, nonceB64,
        ).joinToString("\\n").toByteArray()
        val key = hkdfSha256(shared, sha256("$appKey:\${serverKey.keyId}".toByteArray()), aad, 32)

        // 无请求体的方法（GET/DELETE/HEAD）把 query 加密后放进 ?_payload=，
        // 因为 OkHttp / URLSession / fetch 都拒绝构造带 body 的 GET
        val bodyless = request.method.uppercase() in setOf("GET", "DELETE", "HEAD")
        val plaintext = if (bodyless) {
            request.url.encodedQuery.orEmpty().toByteArray()
        } else {
            Buffer().also { request.body?.writeTo(it) }.readByteArray()
        }
        val payload = b64u(xChaCha20Poly1305Seal(key, nonce, plaintext, aad))

        var builder = request.newBuilder()
            .header("X-Aegis-Protocol", "aegis-transport-v2")
            .header("X-Aegis-Key-Id", serverKey.keyId)
            .header("X-Aegis-Client-Key", b64u(ephemeral.public))
        builder = if (bodyless) {
            val url = request.url.newBuilder().query(null)
                .addQueryParameter(spec.payloadParam, payload).build()
            builder.url(url).method(request.method, null)
        } else {
            builder.method(
                request.method,
                payload.toByteArray().toRequestBody("application/octet-stream".toMediaType()),
            )
        }

        val response = chain.proceed(sign(builder.build(), nonceB64))

        // 网关在拆包**之前**拒掉的请求（签名不符、时间戳过期）返回的是明文 JSON，
        // 没有这个头。不判这一下，接入期最需要看到的错误会全变成「解密失败」
        val responseNonce = response.header("X-Aegis-Response-Nonce") ?: return response
        val responseKey = sha256(key + "aegis-response-v2".toByteArray())
        val responseAad = listOf(
            "aegis-transport-v2", appKey, serverKey.keyId,
            response.code.toString(), nonceB64, responseNonce,
        ).joinToString("\\n").toByteArray()
        val plain = xChaCha20Poly1305Open(
            responseKey, b64uDecode(responseNonce),
            b64uDecode(response.body!!.string().trim()), responseAad,
        )
        return response.newBuilder()
            .body(plain.toResponseBody("application/json".toMediaType())).build()
    }
}`
    },
    {
      lang: "text",
      code: `加密档在签名档基础上叠加，两层同时生效。

发送顺序
  1. 用 X25519 + HKDF-SHA256 + XChaCha20-Poly1305 把 JSON 加密成 base64url 密文
  2. 对该密文按签名档规则计算 HMAC
  3. 密文作为请求体发出，签名放在 X-Aegis-Signature

为什么仍然需要签名
  AEAD 只保证密文未被篡改。服务端公钥是公开的，任何人都能生成临时密钥对
  构造出合法密文，因此加密本身不能证明调用方身份，这部分由签名承担。

三处容易出错的细节
  请求 AAD  七行，\\n 分隔，末尾不带换行；HKDF 的 info 就是这段 AAD
  HKDF 盐   SHA-256("{appKey}:{keyId}")，取公开 appKey，不是内部数字 ID
  响应密钥  SHA-256(requestKey ‖ "aegis-response-v2")，
            响应 AAD 为六行，第四行是 HTTP 状态码

包装对不上时，请应用管理员在控制台运行接入自检。
服务端会按同一套规格实跑一遍，指出是签名、时间戳还是 AAD 出错。`
    }
  ];
}

// ─────────────────────────────────────────────────────────────────────
// 场景 2 起：注册 / 短信 / 第三方 / 会话
//
// 这些场景与登录共用同一套包装，因此统一按标准档展示请求与响应结构。
// 换档时复用上面的 sign() 或 callSealed()，业务字段不变。
// ─────────────────────────────────────────────────────────────────────

function registerScenario({ baseUrl, appKey }: SnippetContext): Snippet[] {
  const base = `${baseUrl}/api/v1/apps/${appKey}`;
  return [
    {
      lang: "curl",
      code: `curl -X POST "${base}/auth/register" \\
  -H "Content-Type: application/json" \\
  -d '{
    "account": "alice",
    "password": "correct horse battery staple",
    "nickname": "Alice",
    "profile": { "company": "Aegis Labs" }
  }'`
    },
    {
      lang: "typescript",
      code: `// 按 /config 的 registrationSchema 动态渲染注册表单，
// 管理员加字段就不需要发版。只有标记 mutable 的字段能通过 profile 提交。
export async function register(form: Record<string, unknown>) {
  const { account, password, nickname, ...profile } = form;
  const res = await fetch(\`\${BASE}/api/v1/apps/\${APP_KEY}/auth/register\`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ account, password, nickname, profile })
  });
  const { code, message, data } = await res.json();
  if (code !== 200) throw new Error(message);
  return data;
}`
    },
    {
      lang: "python",
      code: `def register(account: str, password: str, nickname: str = "", **profile) -> dict:
    envelope = requests.post(
        f"{BASE}/api/v1/apps/{APP_KEY}/auth/register",
        json={
            "account": account,
            "password": password,
            "nickname": nickname,
            "profile": profile,
        },
        timeout=10,
    ).json()
    if envelope["code"] != 200:
        raise RuntimeError(envelope["message"])
    return envelope["data"]`
    }
  ];
}

function smsScenario({ baseUrl, appKey }: SnippetContext): Snippet[] {
  const base = `${baseUrl}/api/v1/apps/${appKey}`;
  return [
    {
      lang: "curl",
      code: `# 1. 申请验证码。purpose 决定这串码只能用于登录还是注册，用途不能混用。
curl -X POST "${base}/auth/sms/code" \\
  -H "Content-Type: application/json" \\
  -d '{"purpose":"login","phone":"13800138000"}'

# 应用开了图形验证码时，先调 /captcha 拿 captchaId 一起带上（防短信轰炸）：
#   -d '{"purpose":"login","phone":"...","captchaId":"...","captchaAnswer":"..."}'

# 2. 手机号 + 验证码登录，响应结构与密码登录相同。
curl -X POST "${base}/auth/login" \\
  -H "Content-Type: application/json" \\
  -d '{"method":"sms","phone":"13800138000","code":"123456"}'

# 手机号未注册时：应用启用了短信注册则自动建号并直接返回会话；
# 未启用则返回 40394。`
    },
    {
      lang: "typescript",
      code: `export async function sendSmsCode(phone: string, purpose: "login" | "register" = "login") {
  const res = await fetch(\`\${BASE}/api/v1/apps/\${APP_KEY}/auth/sms/code\`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ phone, purpose })
  });
  const { code, message } = await res.json();
  if (code !== 200) throw new Error(message);
}

export async function loginBySms(phone: string, smsCode: string) {
  const res = await fetch(\`\${BASE}/api/v1/apps/\${APP_KEY}/auth/login\`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ method: "sms", phone, code: smsCode })
  });
  const { code, message, data } = await res.json();
  if (code !== 200) throw new Error(message);
  return data; // 结构与密码登录相同
}`
    },
    {
      lang: "python",
      code: `def send_sms_code(phone: str, purpose: str = "login") -> None:
    envelope = requests.post(
        f"{BASE}/api/v1/apps/{APP_KEY}/auth/sms/code",
        json={"phone": phone, "purpose": purpose}, timeout=10,
    ).json()
    if envelope["code"] != 200:
        raise RuntimeError(envelope["message"])

def login_by_sms(phone: str, code: str) -> dict:
    envelope = requests.post(
        f"{BASE}/api/v1/apps/{APP_KEY}/auth/login",
        json={"method": "sms", "phone": phone, "code": code}, timeout=10,
    ).json()
    if envelope["code"] != 200:
        raise RuntimeError(envelope["message"])
    return envelope["data"]`
    },
    {
      lang: "text",
      code: `需要额外收集昵称或自定义字段时，走显式注册：

  POST /auth/sms/code   { "purpose": "register", "phone": "13800138000" }
  POST /auth/register   { "method": "sms", "phone": "...", "code": "...",
                          "nickname": "新用户", "profile": { ... } }

手机号即账号，registrationSchema 中的 account 与 password 跳过必填判定。
profile 仍受 schema 约束，只有标记 mutable 的字段可以提交。

短信建号不设密码，这类账号只能用短信登录。
改用密码登录需要走「设置密码」流程。

短信依赖控制台「验证码」页配置的服务商，未配置时 /auth/sms/code 会返回错误。`
    }
  ];
}

function oauthScenario({ baseUrl, appKey }: SnippetContext): Snippet[] {
  const base = `${baseUrl}/api/v1/apps/${appKey}`;
  return [
    {
      lang: "typescript",
      code: `// Web / 系统浏览器跳转流程
//
// 可用渠道读 /config 的 auth.oauthProviders，应用没启用第三方登录时该数组为空。
// 不要在客户端硬编码渠道列表，否则管理员改配置就得发版。
export async function startOAuth(provider: string, deviceId: string) {
  const res = await fetch(\`\${BASE}/api/v1/apps/\${APP_KEY}/auth/oauth/url\`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider, deviceId })
  });
  const { code, message, data } = await res.json();
  if (code !== 200) throw new Error(message);
  window.location.href = data.url;
}

// 第三方授权后回跳到
//   ${base}/auth/oauth/callback?provider=..&code=..&state=..
// 该地址直接返回登录结果信封。把它配成渠道的 redirect_uri。`
    },
    {
      lang: "curl",
      code: `# 1. 取授权地址
curl -X POST "${base}/auth/oauth/url" \\
  -H "Content-Type: application/json" \\
  -d '{"provider":"wechat","deviceId":"device-001"}'

# 2. 浏览器跳到返回的 url，用户授权后第三方回跳：
#    ${base}/auth/oauth/callback?provider=wechat&code=..&state=..

# 3. 原生 SDK 路线：端上拿到 profile 后换会话
curl -X POST "${base}/auth/oauth/exchange" \\
  -H "Content-Type: application/json" \\
  -d '{
    "provider": "wechat",
    "providerUserId": "oGZUI0egBJY1zhBYw2KhdUfwVJJE",
    "unionId": "o6_bmasdasdsad6_2sgVt7hMZOPfL",
    "nickname": "Alice",
    "avatar": "https://example.com/a.png"
  }'`
    },
    {
      lang: "kotlin",
      code: `// 原生 SDK（微信 / QQ / Apple…）在端上完成授权，拿到 profile 后换 Aegis 会话。
// 这条路径走完整的安全等级包装，加密档下同样是加密的。
suspend fun exchangeOAuth(profile: JSONObject): JSONObject = withContext(Dispatchers.IO) {
    val request = Request.Builder()
        .url("$BASE/api/v1/apps/$APP_KEY/auth/oauth/exchange")
        .post(profile.toString().toRequestBody("application/json".toMediaType()))
        .build()

    OkHttpClient().newCall(request).execute().use { response ->
        val envelope = JSONObject(response.body!!.string())
        check(envelope.getInt("code") == 200) { envelope.getString("message") }
        envelope.getJSONObject("data")
    }
}

// 该第三方账号首次登录时能否自动建号，由渠道配置里的「允许自动注册」决定；
// 关闭时返回 40393，此时应引导用户先用已有账号登录再绑定。`
    },
    {
      lang: "text",
      code: `两条路径，按客户端形态选：

  Web / 系统浏览器   /auth/oauth/url → 跳转 → /auth/oauth/callback
  原生 SDK          端上授权 → /auth/oauth/exchange

回跳注意
  /auth/oauth/callback 由第三方平台重定向浏览器发起，客户端无法为它签名或加密，
  因此加密档下这一跳仍是明文。要求全链路加密时请改用原生 SDK 路径，
  /auth/oauth/exchange 受完整包装保护。

自动建号的判定分开配置
  短信    取决于应用的 registerMethods 是否包含 sms
  第三方  取决于该渠道自身的「允许自动注册」开关

第三方没有应用级注册开关，避免同一件事出现两处配置。`
    }
  ];
}

function sessionScenario({ baseUrl, appKey }: SnippetContext): Snippet[] {
  const base = `${baseUrl}/api/v1/apps/${appKey}`;
  return [
    {
      lang: "curl",
      code: `# 刷新（accessToken 过期前用 refreshToken 换新的）
curl -X POST "${base}/auth/refresh" \\
  -H "Content-Type: application/json" \\
  -d '{"refreshToken":"<refreshToken>"}'

# 当前用户
curl "${base}/me" \\
  -H "Authorization: Bearer <accessToken>"

# 注销
curl -X POST "${base}/auth/logout" \\
  -H "Authorization: Bearer <accessToken>"`
    },
    {
      lang: "typescript",
      code: `// 登录成功后所有需要身份的接口都带 Authorization 头
export async function me(accessToken: string) {
  const res = await fetch(\`\${BASE}/api/v1/apps/\${APP_KEY}/me\`, {
    headers: { Authorization: \`Bearer \${accessToken}\` }
  });
  const { code, message, data } = await res.json();
  if (code !== 200) throw new Error(message);
  return data;
}

export async function refresh(refreshToken: string) {
  const res = await fetch(\`\${BASE}/api/v1/apps/\${APP_KEY}/auth/refresh\`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refreshToken })
  });
  const { code, message, data } = await res.json();
  if (code !== 200) throw new Error(message);
  return data;
}`
    },
    {
      lang: "text",
      code: `二次认证

登录响应里 requiresSecondFactor 为 true 时不会签发令牌，
需要拿 challenge.challengeId 调 /auth/2fa/verify 完成验证：

  POST /auth/2fa/verify
  { "challengeId": "...", "code": "123456" }

也可以用恢复码：{ "challengeId": "...", "recoveryCode": "..." }`
    }
  ];
}

// ─────────────────────────────────────────────────────────────────────

function loginSnippets(context: SnippetContext): Snippet[] {
  switch (context.level) {
    case "signed":
      return signedLogin(context);
    case "sealed":
      return sealedLogin(context);
    default:
      return standardLogin(context);
  }
}

const LEVEL_SUMMARY: Record<SecurityLevel, string> = {
  standard: "在 HTTPS 上直接发送 JSON，不涉及密钥与密码学库。",
  signed: "在标准档基础上，为每个请求计算一个 HMAC-SHA256 签名头。",
  sealed: "在签名档基础上加密请求体，响应同样是加密的。"
};

/**
 * 按场景组织的全部示例。
 *
 * 只有「登录」随安全等级变化；其余场景的包装方式与登录一致，
 * 因此统一按标准档展示请求与响应形状。
 */
export function buildScenarios(context: SnippetContext): Scenario[] {
  return [
    {
      id: "login",
      label: "登录",
      summary: LEVEL_SUMMARY[context.level],
      snippets: loginSnippets(context)
    },
    {
      id: "register",
      label: "注册",
      summary: "字段由 /config 的 registrationSchema 下发，客户端据此渲染表单。",
      snippets: registerScenario(context)
    },
    {
      id: "sms",
      label: "短信登录",
      summary: "先取码再登录，响应结构与密码登录相同；未注册的手机号可按策略自动建号。",
      snippets: smsScenario(context)
    },
    {
      id: "oauth",
      label: "第三方登录",
      summary: "Web 端跳转到授权页后回跳，原生 App 用 SDK 授权后调 exchange 换会话。",
      snippets: oauthScenario(context)
    },
    {
      id: "session",
      label: "令牌与会话",
      summary: "令牌刷新、读取当前用户、注销，以及二次认证挑战。",
      snippets: sessionScenario(context)
    },
    {
      id: "sdk",
      label: "官方 SDK",
      summary:
        "Android 与 JVM 服务端共用一份产物；三档包装、时钟校准、密钥轮换重试、令牌刷新都在 SDK 里。",
      snippets: sdkScenario(context)
    }
  ];
}

// ─────────────────────────────────────────────────────────────────────
// 场景 6：官方 Kotlin / Java SDK
//
// 上面几个场景是「协议长什么样」，这一节是「不想自己实现协议时用什么」。
// 手写 transport 适配器要对齐签名、AAD、HKDF、密钥轮换与时钟偏差 ——
// 每一处对不上都表现为一个含糊的 400，SDK 把这些一次性解决掉。
// ─────────────────────────────────────────────────────────────────────

function sdkScenario({ baseUrl, appKey, level }: SnippetContext): Snippet[] {
  const secretLine =
    level === "standard"
      ? ""
      : `\n    .appSecret(System.getenv("AEGIS_APP_SECRET"))  // ${level} 档必需，只放服务端`;
  return [
    {
      lang: "kotlin",
      code: `// build.gradle.kts
// implementation("dev.aegis:aegis-sdk:1.0.0")

val client = AegisClient.builder("${baseUrl}", "${appKey}")${secretLine}
    .tokenStore(prefsTokenStore)   // 默认在内存；Android 换成 EncryptedSharedPreferences
    .build()

// 登录。二次认证时 accessToken 是空的，会话还没建立 ——
// 把这一步当成"登录成功"是最常见的接入错误
val first = client.auth.loginWithPassword("alice", "secret")
val session = if (first.requiresSecondFactor) {
    client.auth.verifySecondFactor(first.challenge!!.challengeId, code = userInput)
} else first

if (session.passwordChangeRequired) goToChangePassword()

// 之后所有接口都在同一个命名空间下，包装由 SDK 处理
val profile = client.me.profile()
val points = client.engagement.pointsOverview()
client.me.uploadAvatar(File(path))          // multipart，sealed 档下整体加密

// 注册表单要按 /config 下发的 schema 渲染，不要硬编码
val schema = client.config().auth.registrationSchema`
    },
    {
      lang: "java",
      code: `// Maven / Gradle: dev.aegis:aegis-sdk:1.0.0

AegisClient client = AegisClient.builder("${baseUrl}", "${appKey}")${secretLine}
        .build();

try {
    AegisSession session = client.getAuth().loginWithPassword("alice", "secret");
    if (session.getRequiresSecondFactor()) {
        session = client.getAuth()
                .verifySecondFactor(session.getChallenge().getChallengeId(), "123456");
    }
    JsonElement profile = client.getMe().profile();
} catch (AegisException error) {
    // 按"调用方该怎么办"分类，而不是按 HTTP 状态码 ——
    // 「签名算错了」和「密码输错了」都是 400，不分类每次都得人肉看 code
    switch (error.getKind()) {
        case BUSINESS:  showToast(error.getMessage()); break;   // 原样显示给用户
        case AUTH:      goToLogin(); break;
        case NETWORK:   showRetry(); break;
        case TRANSPORT: log("接入配置有误：" + error.getHint()); break;
    }
}`
    },
    {
      lang: "text",
      code: `SDK 替你处理掉的三件事（自己实现 transport 时这三处最容易出问题）

  时钟漂移    signed / sealed 档最常见的线上故障：设备时钟偏差超过 5 分钟后
              一路 40071，而用户只看到"登录失败"。SDK 从 /config 的 serverTime
              学到偏移量，之后所有请求都用校准后的时间戳。

  密钥轮换    sealed 档的服务端公钥会轮换，旧钥最多保留 24 小时。收到 40074 时
              自动重拉一次 /config 并重试一次 —— 只重试一次，无限重试只会把
              一个明确的失败拖成一串超时。

  令牌过期    40100 且本地有 refreshToken 时自动刷新并重试一次。

不用 SDK 也完全可以：协议规格在 docs/app-integration.md 里逐字节写明，
上面几个场景就是照着它写的。包装对不上时，请应用管理员运行控制台的「接入自检」，
服务端会按同一套规格实跑一遍，直接告诉你是签名、时间戳还是 AAD 出错。

安全等级与密钥
  standard  移动端 / 前端停在这里。不需要 appSecret，安全性由 HTTPS 提供，
            并不比"把密钥硬编码进 APK"差。
  signed    有自己服务端时用。appSecret 是真正的密钥，只能放服务端。
  sealed    强合规场景。在 signed 之上再叠端到端加密。`
    }
  ];
}

/** 接入卡片的「复制 .env」按钮内容 */
export function buildEnvSnippet({ baseUrl, appKey, level }: SnippetContext) {
  const lines = [
    `AEGIS_BASE_URL=${baseUrl}`,
    `AEGIS_APP_KEY=${appKey}`,
    `AEGIS_SECURITY_LEVEL=${level}`
  ];
  if (level !== "standard") {
    lines.push(`AEGIS_APP_SECRET=${SECRET}`);
  }
  return lines.join("\n");
}
