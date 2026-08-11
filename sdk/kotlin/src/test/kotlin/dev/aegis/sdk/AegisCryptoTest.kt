package dev.aegis.sdk

import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

class AegisCryptoTest {

    /**
     * HChaCha20 是这个 SDK 里唯一手写的密码学原语（JCA 与 BouncyCastle 都没有），
     * 因此必须被官方测试向量钉住，而不是"跑通了就行"。
     * 向量取自 draft-irtf-cfrg-xchacha-03 §2.2.1。
     *
     * 最容易写错的一处是：20 轮之后**不做**与初始状态的加法。
     * 加了照样能产出 32 字节看似合理的子密钥，但服务端一律 40077 认证失败。
     */
    @Test
    fun `HChaCha20 matches the RFC draft test vector`() {
        val key = hex("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
        val nonce = hex("000000090000004a0000000031415927")
        val expected = hex("82413b4227b27bfed30e42508a877d73a0f9e4d58a74a853c12ec41326d3ecdc")
        assertContentEquals(expected, AegisCrypto.hChaCha20(key, nonce))
    }

    @Test
    fun `XChaCha20-Poly1305 round trips with associated data`() {
        val key = AegisCrypto.randomBytes(32)
        val nonce = AegisCrypto.randomBytes(AegisCrypto.NONCE_SIZE)
        val aad = "aegis-transport-v2\ndemo_app".toByteArray()
        val plaintext = """{"account":"alice"}""".toByteArray()

        val sealed = AegisCrypto.seal(key, nonce, plaintext, aad)
        assertEquals(plaintext.size + 16, sealed.size, "AEAD 应当只多出 16 字节 tag")
        assertContentEquals(plaintext, AegisCrypto.open(key, nonce, sealed, aad))
    }

    /** 空明文也必须能封：无查询参数的 GET 发的就是空串的密文。 */
    @Test
    fun `XChaCha20-Poly1305 seals an empty plaintext`() {
        val key = AegisCrypto.randomBytes(32)
        val nonce = AegisCrypto.randomBytes(AegisCrypto.NONCE_SIZE)
        val aad = "aad".toByteArray()

        val sealed = AegisCrypto.seal(key, nonce, ByteArray(0), aad)
        assertEquals(16, sealed.size, "空明文封出来应当只有 tag")
        assertEquals(0, AegisCrypto.open(key, nonce, sealed, aad).size)
    }

    @Test
    fun `AAD mismatch fails authentication`() {
        val key = AegisCrypto.randomBytes(32)
        val nonce = AegisCrypto.randomBytes(AegisCrypto.NONCE_SIZE)
        val sealed = AegisCrypto.seal(key, nonce, "payload".toByteArray(), "aad-a".toByteArray())

        val error = assertFailsWith<AegisException> {
            AegisCrypto.open(key, nonce, sealed, "aad-b".toByteArray())
        }
        assertEquals(AegisException.Kind.TRANSPORT, error.kind)
    }

    @Test
    fun `X25519 agreement matches on both sides`() {
        val client = AegisCrypto.generateEphemeralKeyPair()
        val server = AegisCrypto.generateEphemeralKeyPair()
        assertContentEquals(
            AegisCrypto.sharedSecret(client.privateKey, server.publicKey),
            AegisCrypto.sharedSecret(server.privateKey, client.publicKey),
        )
    }

    /** 服务端下发的是无 padding 的 base64url；别人转手时可能补上，两种都要能解。 */
    @Test
    fun `base64url decoding tolerates padding`() {
        val data = AegisCrypto.randomBytes(30)
        val encoded = AegisCrypto.encodeBase64Url(data)
        assertTrue(!encoded.contains('='), "编码出来不应带 padding")
        assertContentEquals(data, AegisCrypto.decodeBase64Url(encoded))
        assertContentEquals(data, AegisCrypto.decodeBase64Url("$encoded=="))
    }

    @Test
    fun `nonce length falls inside the server accepted range`() {
        val nonce = AegisCrypto.randomNonce()
        assertTrue(nonce.length in 8..128, "nonce 长度 ${nonce.length} 超出服务端接受区间")
    }

    private fun hex(value: String): ByteArray =
        ByteArray(value.length / 2) { value.substring(it * 2, it * 2 + 2).toInt(16).toByte() }
}
