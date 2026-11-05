export const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
export function bytesToBase64(bytes) {
    let result = "";
    for (let index = 0; index < bytes.length; index += 3) {
        const first = bytes[index] ?? 0;
        const hasSecond = index + 1 < bytes.length;
        const second = hasSecond ? bytes[index + 1] ?? 0 : 0;
        const hasThird = index + 2 < bytes.length;
        const third = hasThird ? bytes[index + 2] ?? 0 : 0;
        result += base64Alphabet.charAt(first >> 2);
        result += base64Alphabet.charAt(((first & 3) << 4) | (second >> 4));
        result += hasSecond ? base64Alphabet.charAt(((second & 15) << 2) | (third >> 6)) : "=";
        result += hasThird ? base64Alphabet.charAt(third & 63) : "=";
    }
    return result;
}
//# sourceMappingURL=embedded-base64.js.map