import javax.crypto.Cipher;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;
import java.nio.charset.StandardCharsets;
import java.util.Base64;

/**
 * Reference implementation of the production CryptoUtil (Java 8), used to
 * generate Go/Java interop test vectors for the AES tool.
 */
public class CryptoUtil {
    private static final String ALGORITHM_STR = "AES/GCM/NoPadding";

    public static String aesEncrypt(String secretKey, String iv, String plaintext) throws Exception {
        SecretKeySpec key = new SecretKeySpec(secretKey.getBytes(StandardCharsets.UTF_8), "AES");
        Cipher cipher = Cipher.getInstance(ALGORITHM_STR);
        cipher.init(Cipher.ENCRYPT_MODE, key, new GCMParameterSpec(128, iv.getBytes(StandardCharsets.UTF_8)));
        byte[] enc = cipher.doFinal(plaintext.getBytes(StandardCharsets.UTF_8));
        return Base64.getEncoder().encodeToString(enc);
    }

    public static String aesDecrypt(String secretKey, String iv, String ciphertext) throws Exception {
        SecretKeySpec key = new SecretKeySpec(secretKey.getBytes(StandardCharsets.UTF_8), "AES");
        Cipher cipher = Cipher.getInstance(ALGORITHM_STR);
        cipher.init(Cipher.DECRYPT_MODE, key, new GCMParameterSpec(128, iv.getBytes(StandardCharsets.UTF_8)));
        byte[] dec = cipher.doFinal(Base64.getDecoder().decode(ciphertext));
        return new String(dec, StandardCharsets.UTF_8);
    }

    public static void main(String[] args) throws Exception {
        String mode = args[0], secretKey = args[1], iv = args[2], text = args[3];
        if (mode.equals("encrypt")) {
            System.out.println(aesEncrypt(secretKey, iv, text));
        } else if (mode.equals("decrypt")) {
            System.out.println(aesDecrypt(secretKey, iv, text));
        }
    }
}
