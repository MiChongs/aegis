package dev.aegis.sdk

import org.bouncycastle.crypto.generators.HKDFBytesGenerator
import org.bouncycastle.crypto.digests.SHA256Digest
import org.bouncycastle.crypto.modes.ChaCha20Poly1305
import org.bouncycastle.crypto.params.AEADParameters
import org.bouncycastle.crypto.params.HKDFParameters
import org.bouncycastle.crypto.params.KeyParameter
import org.bouncycastle.math.ec.rfc7748.X25519
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

/**
 * 协议用到的全部密码学原语。
 *
 * 只依赖 BouncyCastle（纯 Java），因此这个 SDK 在 Android 与普通 JVM 服务端上
 * 都能跑，不需要 NDK、不需要 API 33+ 的 XDH、也不引入 JNA/原生库。
 * 服务端接入（signed 档持有 appSecret）与移动端接入用的是同一份实现。
 *
 * XChaCha20-Poly1305 在 JCA 和 BouncyCastle 里都没有现成实现，这里按
 * draft-irtf-cfrg-xchacha 的标准构造拼出来：
 *
 *   subkey = HChaCha20(key, nonce[0..16])
 *   密文   = ChaCha20-Poly1305(subkey, 0x00000000 ‖ nonce[16..24])
 *
 * HChaCha20 就是 ChaCha20 的核心置换去掉最后的加法，见 [hChaCha20]。
 * 它被 RFC 草案的官方测试向量钉住（AegisCryptoTest），不是"看起来对"。
 */
internal object AegisCrypto {

    private val random = SecureRandom()
    private val base64Url: Base64.Encoder = Base64.getUrlEncoder().withoutPadding()
    private val base64UrlDecoder: Base64.Decoder = Base64.getUrlDecoder()

    const val NONCE_SIZE = 24
    const val KEY_SIZE = 32
    private const val TAG_SIZE_BITS = 128

    // ── 编码 ──────────────────────────────────────────────────────────

    fun encodeBase64Url(data: ByteArray): String = base64Url.encodeToString(data)

    /** 兼容有无 padding 两种写法：服务端一律发无 padding，但别人转手时可能补上。 */
    fun decodeBase64Url(value: String): ByteArray {
        val trimmed = value.trim().trimEnd('=')
        return base64UrlDecoder.decode(trimmed)
    }

    fun randomBytes(size: Int): ByteArray = ByteArray(size).also(random::nextBytes)

    /** signed 档的 nonce：22 字符 base64url，落在服务端要求的 8–128 区间内。 */
    fun randomNonce(): String = encodeBase64Url(randomBytes(16))

    // ── 摘要与 MAC ────────────────────────────────────────────────────

    fun sha256(data: ByteArray): ByteArray =
        MessageDigest.getInstance("SHA-256").digest(data)

    fun sha256Hex(data: ByteArray): String = toHex(sha256(data))

    fun hmacSha256Hex(secret: String, message: String): String {
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(secret.toByteArray(Charsets.UTF_8), "HmacSHA256"))
        return toHex(mac.doFinal(message.toByteArray(Charsets.UTF_8)))
    }

    private fun toHex(data: ByteArray): String {
        val builder = StringBuilder(data.size * 2)
        for (byte in data) {
            builder.append("0123456789abcdef"[(byte.toInt() shr 4) and 0x0F])
            builder.append("0123456789abcdef"[byte.toInt() and 0x0F])
        }
        return builder.toString()
    }

    // ── X25519 ────────────────────────────────────────────────────────

    class KeyPair(val privateKey: ByteArray, val publicKey: ByteArray)

    /** 每次 sealed 请求都要一对全新的临时密钥：复用会让多次请求可被关联。 */
    fun generateEphemeralKeyPair(): KeyPair {
        val privateKey = ByteArray(X25519.SCALAR_SIZE)
        X25519.generatePrivateKey(random, privateKey)
        val publicKey = ByteArray(X25519.POINT_SIZE)
        X25519.generatePublicKey(privateKey, 0, publicKey, 0)
        return KeyPair(privateKey, publicKey)
    }

    fun sharedSecret(privateKey: ByteArray, peerPublicKey: ByteArray): ByteArray {
        require(peerPublicKey.size == X25519.POINT_SIZE) {
            "服务端公钥必须是 32 字节，实际 ${peerPublicKey.size}"
        }
        val shared = ByteArray(X25519.POINT_SIZE)
        if (!X25519.calculateAgreement(privateKey, 0, peerPublicKey, 0, shared, 0)) {
            throw AegisException.transport("X25519 密钥协商失败：服务端公钥不合法")
        }
        return shared
    }

    /** HKDF-SHA256，L=32。salt 与 info 由 [AegisCanonical] 给出。 */
    fun deriveKey(sharedSecret: ByteArray, salt: ByteArray, info: ByteArray): ByteArray {
        val generator = HKDFBytesGenerator(SHA256Digest())
        generator.init(HKDFParameters(sharedSecret, salt, info))
        return ByteArray(KEY_SIZE).also { generator.generateBytes(it, 0, it.size) }
    }

    /** 响应密钥 = SHA-256(请求密钥 ‖ "aegis-response-v2")。 */
    fun deriveResponseKey(requestKey: ByteArray): ByteArray =
        sha256(requestKey + AegisCanonical.RESPONSE_KEY_INFO.toByteArray(Charsets.UTF_8))

    // ── XChaCha20-Poly1305 ────────────────────────────────────────────

    fun seal(key: ByteArray, nonce: ByteArray, plaintext: ByteArray, aad: ByteArray): ByteArray =
        xChaCha20Poly1305(true, key, nonce, plaintext, aad)

    fun open(key: ByteArray, nonce: ByteArray, ciphertext: ByteArray, aad: ByteArray): ByteArray =
        xChaCha20Poly1305(false, key, nonce, ciphertext, aad)

    private fun xChaCha20Poly1305(
        forEncryption: Boolean,
        key: ByteArray,
        nonce: ByteArray,
        input: ByteArray,
        aad: ByteArray,
    ): ByteArray {
        require(key.size == KEY_SIZE) { "密钥必须是 32 字节" }
        require(nonce.size == NONCE_SIZE) { "XChaCha20 的 nonce 必须是 24 字节" }

        val subKey = hChaCha20(key, nonce.copyOfRange(0, 16))
        // RFC 8439 的 12 字节 nonce：前 4 字节补零，后 8 字节取原 nonce 的尾部。
        val innerNonce = ByteArray(12)
        nonce.copyInto(innerNonce, destinationOffset = 4, startIndex = 16, endIndex = 24)

        val engine = ChaCha20Poly1305()
        engine.init(forEncryption, AEADParameters(KeyParameter(subKey), TAG_SIZE_BITS, innerNonce, aad))
        val output = ByteArray(engine.getOutputSize(input.size))
        val written = engine.processBytes(input, 0, input.size, output, 0)
        val finished = try {
            engine.doFinal(output, written)
        } catch (error: Exception) {
            // 解密失败一律是认证失败：密钥、nonce、AAD 三者任一对不上都会走到这里。
            throw AegisException.transport("载荷认证失败：核对 AAD 拼接、HKDF 盐与 nonce", error)
        }
        return if (written + finished == output.size) output else output.copyOf(written + finished)
    }

    /**
     * HChaCha20：ChaCha20 的核心置换，20 轮之后**不做**与初始状态的加法，
     * 取 state[0..3] 与 state[12..15] 作为 32 字节子密钥。
     *
     * 少了「不做最后加法」这一条就会得到一个看似合理却与服务端对不上的子密钥，
     * 表现为「密文能造出来，服务端一律 40077」。官方测试向量守着这件事。
     */
    internal fun hChaCha20(key: ByteArray, nonce16: ByteArray): ByteArray {
        require(key.size == 32) { "HChaCha20 的密钥必须是 32 字节" }
        require(nonce16.size == 16) { "HChaCha20 的 nonce 必须是 16 字节" }

        val state = IntArray(16)
        state[0] = 0x61707865
        state[1] = 0x3320646e
        state[2] = 0x79622d32
        state[3] = 0x6b206574
        for (i in 0 until 8) state[4 + i] = readLittleEndianInt(key, i * 4)
        for (i in 0 until 4) state[12 + i] = readLittleEndianInt(nonce16, i * 4)

        repeat(10) {
            quarterRound(state, 0, 4, 8, 12)
            quarterRound(state, 1, 5, 9, 13)
            quarterRound(state, 2, 6, 10, 14)
            quarterRound(state, 3, 7, 11, 15)
            quarterRound(state, 0, 5, 10, 15)
            quarterRound(state, 1, 6, 11, 12)
            quarterRound(state, 2, 7, 8, 13)
            quarterRound(state, 3, 4, 9, 14)
        }

        val out = ByteArray(32)
        for (i in 0 until 4) writeLittleEndianInt(out, i * 4, state[i])
        for (i in 0 until 4) writeLittleEndianInt(out, 16 + i * 4, state[12 + i])
        return out
    }

    private fun quarterRound(state: IntArray, a: Int, b: Int, c: Int, d: Int) {
        state[a] += state[b]; state[d] = Integer.rotateLeft(state[d] xor state[a], 16)
        state[c] += state[d]; state[b] = Integer.rotateLeft(state[b] xor state[c], 12)
        state[a] += state[b]; state[d] = Integer.rotateLeft(state[d] xor state[a], 8)
        state[c] += state[d]; state[b] = Integer.rotateLeft(state[b] xor state[c], 7)
    }

    private fun readLittleEndianInt(data: ByteArray, offset: Int): Int =
        (data[offset].toInt() and 0xFF) or
            ((data[offset + 1].toInt() and 0xFF) shl 8) or
            ((data[offset + 2].toInt() and 0xFF) shl 16) or
            ((data[offset + 3].toInt() and 0xFF) shl 24)

    private fun writeLittleEndianInt(data: ByteArray, offset: Int, value: Int) {
        data[offset] = value.toByte()
        data[offset + 1] = (value ushr 8).toByte()
        data[offset + 2] = (value ushr 16).toByte()
        data[offset + 3] = (value ushr 24).toByte()
    }
}
